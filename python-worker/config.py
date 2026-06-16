import os
from dotenv import load_dotenv

load_dotenv()

class Config:
    RABBITMQ_URL = os.getenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
    TASK_QUEUE = os.getenv("TASK_QUEUE", "thz_tasks")
    RESULT_QUEUE = os.getenv("RESULT_QUEUE", "thz_results")
    ONNX_MODEL_PATH = os.getenv("ONNX_MODEL_PATH", "./models/plsr_model.onnx")
    CELERY_BROKER_URL = os.getenv("CELERY_BROKER_URL", "amqp://guest:guest@localhost:5672//")
    CELERY_RESULT_BACKEND = os.getenv("CELERY_RESULT_BACKEND", "rpc://")

config = Config()
