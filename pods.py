from airflow import DAG
from airflow.providers.amazon.aws.hooks.base_aws import AwsBaseHook
from airflow.operators.python import PythonOperator
from kubernetes import client
import boto3

def check_pods(**context):
    # get aws creds from airflow connection
    aws_hook = AwsBaseHook(aws_conn_id="aws_conn", client_type="sts")
    creds = aws_hook.get_credentials()

    session = boto3.session.Session(
        aws_access_key_id=creds.access_key,
        aws_secret_access_key=creds.secret_key,
        aws_session_token=creds.token,
        region_name="us-east-1",
    )

    eks = session.client("eks")
    cluster_name = "my-eks-cluster"

    token = eks.get_token(clusterName=cluster_name)["token"]

    # configure k8s client
    cluster_info = eks.describe_cluster(name=cluster_name)["cluster"]
    configuration = client.Configuration()
    configuration.host = cluster_info["endpoint"]
    configuration.ssl_ca_cert = "/tmp/ca.crt"
    with open(configuration.ssl_ca_cert, "w") as f:
        f.write(cluster_info["certificateAuthority"]["data"])

    configuration.api_key = {"authorization": f"Bearer {token}"}
    v1 = client.CoreV1Api(client.ApiClient(configuration))

    pods = v1.list_pod_for_all_namespaces(watch=False)
    return [p.metadata.name for p in pods.items]

with DAG("check_eks_pods", start_date=datetime(2024,1,1), schedule="@once", catchup=False) as dag:
    check = PythonOperator(
        task_id="check_pods",
        python_callable=check_pods,
        provide_context=True,
    )
