from airflow import DAG
from airflow.operators.python import PythonOperator
from datetime import datetime, timedelta
from kubernetes import client, config

def get_k8s_client():
    """Load Kubernetes config (in-cluster or kubeconfig)."""
    try:
        config.load_incluster_config()
    except:
        config.load_kube_config()
    return client.CoreV1Api(), client.CoreV1Api()

def check_pod_failures(**kwargs):
    v1, _ = get_k8s_client()
    pods = v1.list_pod_for_all_namespaces(watch=False)

    failed_pods = []
    for pod in pods.items:
        name = pod.metadata.name
        ns = pod.metadata.namespace
        phase = pod.status.phase
        restarts = sum([c.restart_count for c in pod.status.container_statuses or []])

        if phase in ["Failed", "Unknown"] or restarts > 3:
            failed_pods.append({
                "namespace": ns,
                "name": name,
                "phase": phase,
                "restarts": restarts,
            })

    if failed_pods:
        print(f"🚨 Found {len(failed_pods)} problematic pods")
        for p in failed_pods:
            print(p)

    # push to XCom
    return failed_pods


def analyze_root_cause(**kwargs):
    v1, _ = get_k8s_client()
    ti = kwargs['ti']
    failed_pods = ti.xcom_pull(task_ids='check_k8s_pods')

    if not failed_pods:
        print("✅ No failed pods to analyze")
        return

    rca_results = []
    for pod in failed_pods:
        ns = pod["namespace"]
        name = pod["name"]

        # Get events for pod
        events = v1.list_namespaced_event(ns, field_selector=f"involvedObject.name={name}")
        reasons = [e.reason for e in events.items]
        messages = [e.message for e in events.items]

        # Get logs of first container (if available)
        try:
            log = v1.read_namespaced_pod_log(name, ns, tail_lines=20)
        except Exception as e:
            log = f"Could not fetch logs: {e}"

        rca_results.append({
            "namespace": ns,
            "pod": name,
            "phase": pod["phase"],
            "restarts": pod["restarts"],
            "reasons": reasons,
            "messages": messages,
            "last_logs": log,
        })

    # Print results (or forward to Slack/Portkey later)
    for r in rca_results:
        print("🔎 RCA for Pod:", r["pod"])
        print("Reasons:", r["reasons"])
        print("Messages:", r["messages"])
        print("Last Logs:", r["last_logs"][:200], "...")

    return rca_results


default_args = {
    'owner': 'airflow',
    'depends_on_past': False,
    'start_date': datetime(2025, 1, 1),
    'retries': 1,
    'retry_delay': timedelta(minutes=5),
}

with DAG(
    dag_id='k8s_pod_failure_with_rca',
    default_args=default_args,
    schedule_interval='*/10 * * * *',
    catchup=False,
    tags=['monitoring', 'kubernetes', 'rca'],
) as dag:

    check_pods = PythonOperator(
        task_id='check_k8s_pods',
        python_callable=check_pod_failures,
    )

    analyze_rca = PythonOperator(
        task_id='analyze_pod_rca',
        python_callable=analyze_root_cause,
        provide_context=True,
    )

    check_pods >> analyze_rca
