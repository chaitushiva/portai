from datetime import datetime, timedelta
import boto3
from kubernetes import client
from airflow import DAG
from airflow.operators.python import PythonOperator


# Config
CLUSTER_NAME = "my-cluster"
REGION = "us-east-1"
PROBLEM_STATES = {"CrashLoopBackOff", "Error", "Failed"}
RESTART_THRESHOLD = 3


def verify_aws_credentials(**context):
    """List S3 buckets to ensure AWS creds are valid."""
    session = boto3.session.Session(region_name=REGION)
    s3 = session.client("s3")
    buckets = s3.list_buckets()
    bucket_names = [b["Name"] for b in buckets["Buckets"]]
    context["ti"].xcom_push(key="s3_buckets", value=bucket_names)
    return f"✅ AWS creds are valid. Found {len(bucket_names)} buckets."


def get_k8s_client(cluster_name: str, region: str = REGION):
    """Create Kubernetes client using boto3 token for EKS."""
    session = boto3.session.Session()
    eks = session.client("eks", region_name=region)

    cluster_info = eks.describe_cluster(name=cluster_name)["cluster"]

    token = boto3.client("eks", region_name=region).get_token(clusterName=cluster_name)["token"]

    configuration = client.Configuration()
    configuration.host = cluster_info["endpoint"]
    configuration.verify_ssl = True
    configuration.api_key = {"authorization": f"Bearer {token}"}

    return client.CoreV1Api(client.ApiClient(configuration))


def check_pods(**context):
    """Check pods for errors/restarts and push problematic ones to XCom."""
    v1 = get_k8s_client(CLUSTER_NAME, REGION)
    pods = v1.list_pod_for_all_namespaces(watch=False)

    problems = []
    for pod in pods.items:
        state = pod.status.phase
        restarts = sum(cs.restart_count for cs in pod.status.container_statuses or [])

        if state in PROBLEM_STATES or restarts > RESTART_THRESHOLD:
            problems.append({
                "namespace": pod.metadata.namespace,
                "name": pod.metadata.name,
                "state": state,
                "restarts": restarts,
                "reason": (pod.status.reason or "Unknown"),
            })

    context["ti"].xcom_push(key="problematic_pods", value=problems)


def analyze_problems(**context):
    """Perform RCA on problematic pods."""
    problems = context["ti"].xcom_pull(key="problematic_pods", task_ids="check_pods")

    if not problems:
        return "✅ No problematic pods found."

    rca_report = []
    for p in problems:
        reason = p.get("reason", "Unknown")
        if "CrashLoopBackOff" in p["state"]:
            analysis = "Likely bad image or app crash"
        elif p["restarts"] > RESTART_THRESHOLD:
            analysis = "Unstable container / resource limits?"
        else:
            analysis = f"Investigate: {reason}"

        rca_report.append({
            "namespace": p["namespace"],
            "pod": p["name"],
            "state": p["state"],
            "restarts": p["restarts"],
            "analysis": analysis
        })

    return rca_report


# Airflow DAG setup
default_args = {
    "owner": "airflow",
    "depends_on_past": False,
    "email_on_failure": False,
    "email_on_retry": False,
    "retries": 1,
    "retry_delay": timedelta(minutes=5),
}

with DAG(
    dag_id="eks_pod_monitoring_with_s3_check",
    default_args=default_args,
    description="Validate AWS creds, monitor EKS pods for failures, and perform RCA",
    schedule_interval="@hourly",
    start_date=datetime(2025, 1, 1),
    catchup=False,
    tags=["eks", "monitoring", "aws"],
) as dag:

    verify_aws_credentials_task = PythonOperator(
        task_id="verify_aws_credentials",
        python_callable=verify_aws_credentials,
        provide_context=True,
    )

    check_pods_task = PythonOperator(
        task_id="check_pods",
        python_callable=check_pods,
        provide_context=True,
    )

    analyze_problems_task = PythonOperator(
        task_id="analyze_problems",
        python_callable=analyze_problems,
        provide_context=True,
    )

    verify_aws_credentials_task >> check_pods_task >> analyze_problems_task
