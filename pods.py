from airflow import DAG
from airflow.operators.python import PythonOperator
from datetime import datetime, timedelta
import boto3
from kubernetes import client, config

default_args = {
    "owner": "airflow",
    "depends_on_past": False,
    "retries": 1,
    "retry_delay": timedelta(minutes=5),
}

def list_s3():
    # picks env creds automatically
    s3 = boto3.client("s3")
    buckets = s3.list_buckets()
    return [b["Name"] for b in buckets["Buckets"]]

def check_pods():
    # load kubeconfig (mounted from host)
    config.load_kube_config(config_file="/home/airflow/.kube/config")
    v1 = client.CoreV1Api()
    pods = v1.list_pod_for_all_namespaces()
    return [(p.metadata.namespace, p.metadata.name, p.status.phase) for p in pods.items]

with DAG(
    "aws_k8s_test",
    default_args=default_args,
    description="Test AWS + K8s creds in worker",
    schedule_interval=None,
    start_date=datetime(2025, 1, 1),
    catchup=False,
) as dag:

    list_s3_task = PythonOperator(
        task_id="list_s3",
        python_callable=list_s3,
    )

    check_pods_task = PythonOperator(
        task_id="check_pods",
        python_callable=check_pods,
    )

    list_s3_task >> check_pods_task
