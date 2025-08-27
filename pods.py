"""
Airflow DAG to test AWS creds (S3 list) and EKS access (list pods).
"""

import base64
import boto3
from botocore.signers import RequestSigner
from airflow import DAG
from airflow.providers.amazon.aws.hooks.base_aws import AwsBaseHook
from airflow.operators.python import PythonOperator
from kubernetes import client
from datetime import datetime


CLUSTER_NAME = "your-eks-cluster"
REGION = "us-east-1"


def get_k8s_client(aws_conn_id: str, cluster_name: str, region: str = "us-east-1"):
    """Return a Kubernetes CoreV1Api client authenticated with EKS IAM token."""

    # Get creds from Airflow AWS Connection
    aws_hook = AwsBaseHook(aws_conn_id=aws_conn_id, client_type="sts")
    session = aws_hook.get_session()
    eks = session.client("eks", region_name=region)

    # Cluster info
    cluster_info = eks.describe_cluster(name=cluster_name)["cluster"]
    endpoint = cluster_info["endpoint"]
    cert_data = cluster_info["certificateAuthority"]["data"]

    # Generate token
    service_id = eks.meta.service_model.service_id
    signer = RequestSigner(
        service_id,
        region,
        "sts",
        "v4",
        session.get_credentials(),
        session.events,
    )

    token = signer.generate_presigned_url(
        "get_caller_identity",
        Params={},
        region_name=region,
        expires_in=60,
        operation_name="",
    )

    # Configure k8s client
    k8s_config = client.Configuration()
    k8s_config.host = endpoint
    k8s_config.verify_ssl = True
    k8s_config.ssl_ca_cert = None
    k8s_config.api_key = {"authorization": "Bearer " + token}
    k8s_config.ssl_ca_cert_data = base64.b64decode(cert_data).decode()

    return client.CoreV1Api(client.ApiClient(k8s_config))


def list_s3_buckets(**context):
    aws_hook = AwsBaseHook(aws_conn_id="my_aws_conn", client_type="s3")
    s3 = aws_hook.get_client_type("s3")
    buckets = [b["Name"] for b in s3.list_buckets()["Buckets"]]
    print("✅ S3 Buckets:", buckets)


def list_pods(**context):
    k8s = get_k8s_client(
        aws_conn_id="my_aws_conn", cluster_name=CLUSTER_NAME, region=REGION
    )
    pods = k8s.list_pod_for_all_namespaces(watch=False)
    print("✅ Pods found:")
    for p in pods.items:
        print(f" - {p.metadata.namespace}/{p.metadata.name}")


with DAG(
    dag_id="eks_access_check",
    start_date=datetime(2024, 1, 1),
    schedule=None,
    catchup=False,
    tags=["eks", "aws", "kubernetes"],
) as dag:

    s3_task = PythonOperator(
        task_id="list_s3_buckets",
        python_callable=list_s3_buckets,
    )

    pods_task = PythonOperator(
        task_id="list_pods",
        python_callable=list_pods,
    )

    s3_task >> pods_task
