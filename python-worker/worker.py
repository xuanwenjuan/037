import os
import sys
import json
import time
import signal
import threading
from datetime import datetime
from typing import Dict, Optional

import pika
from config import config

from algorithms import FFTProcessor, DorneyDuvillaret, PLSRPredictor

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))


class THZWorker:
    def __init__(self):
        self.connection = None
        self.channel = None
        self.fft_processor = FFTProcessor()
        self.dorney = DorneyDuvillaret()
        self.plsr_predictor = PLSRPredictor(config.ONNX_MODEL_PATH)
        self.running = False
        self.threads = []

    def connect(self):
        params = pika.URLParameters(config.RABBITMQ_URL)
        params.connection_attempts = 5
        params.retry_delay = 5

        self.connection = pika.BlockingConnection(params)
        self.channel = self.connection.channel()

        self.channel.queue_declare(queue=config.TASK_QUEUE, durable=True)
        self.channel.queue_declare(queue=config.RESULT_QUEUE, durable=True)

        self.channel.basic_qos(prefetch_count=1)

        print("Worker connected to RabbitMQ")

    def disconnect(self):
        self.running = False
        if self.channel and self.channel.is_open:
            self.channel.close()
        if self.connection and self.connection.is_open:
            self.connection.close()
        print("Worker disconnected from RabbitMQ")

    def send_result(self, result_data: Dict) -> None:
        try:
            self.channel.basic_publish(
                exchange="",
                routing_key=config.RESULT_QUEUE,
                body=json.dumps(result_data),
                properties=pika.BasicProperties(
                    delivery_mode=2,
                    content_type="application/json",
                ),
            )
        except Exception as e:
            print(f"Failed to send result: {e}")
            self.connect()
            self.channel.basic_publish(
                exchange="",
                routing_key=config.RESULT_QUEUE,
                body=json.dumps(result_data),
                properties=pika.BasicProperties(
                    delivery_mode=2,
                    content_type="application/json",
                ),
            )

    def send_progress(
        self,
        analysis_id: str,
        status: str,
        progress: int,
        stage: str,
        fft: Optional[Dict] = None,
        params: Optional[Dict] = None,
        moisture: Optional[float] = None,
        error: Optional[str] = None,
    ) -> None:
        result = {
            "analysis_id": analysis_id,
            "status": status,
            "progress": progress,
            "stage": stage,
            "timestamp": datetime.utcnow().isoformat(),
        }

        if fft is not None:
            result["fft"] = fft
        if params is not None:
            result["params"] = params
        if moisture is not None:
            result["moisture"] = moisture
        if error is not None:
            result["error"] = error

        self.send_result(result)

    def process_task(self, task_data: Dict) -> None:
        analysis_id = task_data["analysis_id"]
        waveform = task_data["waveform"]
        sample_thickness = task_data["sample_thickness_mm"]

        print(f"Processing task: {analysis_id}")

        try:
            self.send_progress(
                analysis_id,
                "processing",
                5,
                "Starting waveform processing",
            )

            self.send_progress(
                analysis_id,
                "processing",
                10,
                "Performing FFT transformation",
            )

            fft_result = self.fft_processor.process_waveform(
                time=waveform["time"],
                sample_field=waveform["sample_field"],
                reference_field=waveform.get("reference_field"),
            )

            fft_output = {
                "frequencies": fft_result["frequencies"],
                "sample_amplitude": fft_result["sample_amplitude"],
                "sample_phase": fft_result["sample_phase"],
                "reference_amplitude": fft_result.get("reference_amplitude"),
                "reference_phase": fft_result.get("reference_phase"),
            }

            self.send_progress(
                analysis_id,
                "fft_done",
                40,
                "FFT transformation completed",
                fft=fft_output,
            )

            self.send_progress(
                analysis_id,
                "processing",
                45,
                "Extracting optical parameters",
            )

            params_result = self.dorney.extract_parameters(
                frequencies=fft_result["frequencies"],
                sample_amp=fft_result["sample_amplitude"],
                sample_phase=fft_result["sample_phase"],
                reference_amp=fft_result.get("reference_amplitude"),
                reference_phase=fft_result.get("reference_phase"),
                sample_thickness_mm=sample_thickness,
            )

            params_output = {
                "frequencies": params_result["frequencies"],
                "absorption_coeff": params_result["absorption_coeff"],
                "refractive_index": params_result["refractive_index"],
                "extinction_coeff": params_result.get("extinction_coeff"),
            }

            self.send_progress(
                analysis_id,
                "params_done",
                75,
                "Optical parameters extracted",
                params=params_output,
            )

            self.send_progress(
                analysis_id,
                "processing",
                80,
                "Predicting moisture content",
            )

            moisture = self.plsr_predictor.predict(
                frequencies=params_result["frequencies"],
                absorption_coeff=params_result["absorption_coeff"],
                refractive_index=params_result["refractive_index"],
            )

            self.send_progress(
                analysis_id,
                "completed",
                100,
                "Analysis completed successfully",
                moisture=moisture,
            )

            print(f"Task {analysis_id} completed: moisture = {moisture:.4f}%")

        except Exception as e:
            print(f"Error processing task {analysis_id}: {e}")
            import traceback
            traceback.print_exc()

            self.send_progress(
                analysis_id,
                "failed",
                0,
                "Analysis failed",
                error=str(e),
            )

    def callback(self, ch, method, properties, body):
        try:
            task_data = json.loads(body)
            print(f"Received task: {task_data.get('analysis_id')}")

            self.process_task(task_data)

            ch.basic_ack(delivery_tag=method.delivery_tag)

        except Exception as e:
            print(f"Error in callback: {e}")
            ch.basic_nack(delivery_tag=method.delivery_tag, requeue=False)

    def start_consumer(self):
        self.connect()
        self.running = True

        self.channel.basic_consume(
            queue=config.TASK_QUEUE,
            on_message_callback=self.callback,
            auto_ack=False,
        )

        print(f"Worker started, waiting for tasks on queue: {config.TASK_QUEUE}")
        print("Press Ctrl+C to stop")

        try:
            self.channel.start_consuming()
        except KeyboardInterrupt:
            print("Stopping worker...")
            self.disconnect()

    def start_multi_threaded(self, num_threads: int = 3):
        self.running = True

        def worker_thread(thread_id):
            print(f"Starting worker thread {thread_id}")

            while self.running:
                try:
                    connection = pika.BlockingConnection(pika.URLParameters(config.RABBITMQ_URL))
                    channel = connection.channel()
                    channel.queue_declare(queue=config.TASK_QUEUE, durable=True)
                    channel.basic_qos(prefetch_count=1)

                    def callback(ch, method, properties, body):
                        try:
                            task_data = json.loads(body)
                            print(f"Thread {thread_id}: Received task {task_data.get('analysis_id')}")
                            self.process_task(task_data)
                            ch.basic_ack(delivery_tag=method.delivery_tag)
                        except Exception as e:
                            print(f"Thread {thread_id}: Error: {e}")
                            ch.basic_nack(delivery_tag=method.delivery_tag, requeue=False)

                    channel.basic_consume(
                        queue=config.TASK_QUEUE,
                        on_message_callback=callback,
                        auto_ack=False,
                    )

                    print(f"Thread {thread_id}: Ready")
                    channel.start_consuming()

                except Exception as e:
                    if self.running:
                        print(f"Thread {thread_id}: Connection error, reconnecting... {e}")
                        time.sleep(5)

        for i in range(num_threads):
            t = threading.Thread(target=worker_thread, args=(i,), daemon=True)
            t.start()
            self.threads.append(t)

        print(f"Started {num_threads} worker threads")

        try:
            while True:
                time.sleep(1)
        except KeyboardInterrupt:
            print("Stopping all workers...")
            self.running = False
            for t in self.threads:
                t.join(timeout=5)
            print("All workers stopped")


def signal_handler(signum, frame):
    print(f"\nReceived signal {signum}, shutting down...")
    raise KeyboardInterrupt


if __name__ == "__main__":
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)

    worker = THZWorker()

    import argparse
    parser = argparse.ArgumentParser(description="THz Processing Worker")
    parser.add_argument(
        "--threads",
        type=int,
        default=3,
        help="Number of worker threads (default: 3)",
    )
    parser.add_argument(
        "--celery",
        action="store_true",
        help="Run as Celery worker instead of standalone",
    )
    args = parser.parse_args()

    if args.celery:
        print("Starting Celery worker...")
        from celery_app import app
        app.worker_main(["worker", "--loglevel=info", "--concurrency", str(args.threads)])
    else:
        if args.threads > 1:
            worker.start_multi_threaded(num_threads=args.threads)
        else:
            worker.start_consumer()
