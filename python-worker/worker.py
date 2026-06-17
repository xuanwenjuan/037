import os
import sys
import json
import time
import signal
import threading
import hashlib
from datetime import datetime
from typing import Dict, Optional

import pika
import redis
from config import config

from algorithms import (
    FFTProcessor, DorneyDuvillaret, PLSRPredictor,
    BandCutter, AnomalyDetector, DifferentialAnalyzer
)

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))


class THZWorker:
    def __init__(self):
        self.connection = None
        self.channel = None
        self.fft_processor = FFTProcessor()
        self.dorney = DorneyDuvillaret()
        self.plsr_predictor = PLSRPredictor(config.ONNX_MODEL_PATH, use_band_cutting=True)
        self.band_cutter = BandCutter(
            min_freq_thz=0.1,
            max_freq_thz=4.0,
            snr_threshold=3.0,
        )
        self.anomaly_detector = AnomalyDetector(contamination=0.1)
        self.differential_analyzer = DifferentialAnalyzer()
        self.redis_client = None
        if config.ENABLE_CACHE:
            try:
                self.redis_client = redis.Redis.from_url(config.REDIS_URL)
                self.redis_client.ping()
                print("Redis cache connected successfully")
            except Exception as e:
                print(f"WARNING: Failed to connect to Redis: {e}, cache disabled")
                self.redis_client = None
        self.running = False
        self.threads = []
        self._valid_sample_count = 0

    @staticmethod
    def compute_waveform_md5(time_points, sample_field):
        h = hashlib.md5()
        time_bytes = json.dumps(time_points, sort_keys=True).encode()
        sample_bytes = json.dumps(sample_field, sort_keys=True).encode()
        h.update(time_bytes)
        h.update(sample_bytes)
        return h.hexdigest()

    def connect(self):
        params = pika.URLParameters(config.RABBITMQ_URL)
        params.connection_attempts = 5
        params.retry_delay = 5

        self.connection = pika.BlockingConnection(params)
        self.channel = self.connection.channel()

        self.channel.queue_declare(queue=config.TASK_QUEUE, durable=True)
        self.channel.queue_declare(queue=config.RESULT_QUEUE, durable=True)
        self.channel.queue_declare(queue=config.DIFF_TASK_QUEUE, durable=True)
        self.channel.queue_declare(queue=config.DIFF_RESULT_QUEUE, durable=True)

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

        perf_times = {
            "fft_start": 0,
            "fft_end": 0,
            "params_start": 0,
            "params_end": 0,
            "prediction_start": 0,
            "prediction_end": 0,
            "total_start": time.time(),
        }

        try:
            if self.redis_client is not None:
                md5 = self.compute_waveform_md5(waveform["time"], waveform["sample_field"])
                cache_key = f"thz:cache:{md5}"
                cached = self.redis_client.get(cache_key)
                if cached:
                    print(f"Task {analysis_id}: Cache hit for MD5 {md5}")
                    self.send_progress(
                        analysis_id,
                        "processing",
                        5,
                        "Cache hit, checking existing results",
                    )
                    cached_data = json.loads(cached)
                    cached_data["is_cached"] = True
                    cached_data["md5"] = md5
                    cached_data["analysis_id"] = analysis_id
                    self.send_result(cached_data)
                    self.redis_client.incr(f"{cache_key}:hits")
                    return

            self.send_progress(
                analysis_id,
                "processing",
                3,
                "Starting waveform quality check",
            )

            anomaly_result = self.anomaly_detector.detect_anomaly(
                time=waveform["time"],
                field=waveform["sample_field"],
                sample_thickness_mm=sample_thickness,
            )

            self.send_progress(
                analysis_id,
                "processing",
                5,
                "Waveform quality check completed",
            )

            self.send_progress(
                analysis_id,
                "processing",
                10,
                "Performing FFT transformation",
            )

            perf_times["fft_start"] = time.time()
            fft_result = self.fft_processor.process_waveform(
                time=waveform["time"],
                sample_field=waveform["sample_field"],
                reference_field=waveform.get("reference_field"),
            )
            perf_times["fft_end"] = time.time()

            self.send_progress(
                analysis_id,
                "processing",
                20,
                "Performing intelligent band cutting",
            )

            cut_spectrum = self.band_cutter.cut_spectrum(
                frequencies=fft_result["frequencies"],
                sample_amp=fft_result["sample_amplitude"],
                sample_phase=fft_result["sample_phase"],
                reference_amp=fft_result.get("reference_amplitude"),
                reference_phase=fft_result.get("reference_phase"),
            )

            band_info = cut_spectrum["band_info"]
            speedup_fft = cut_spectrum["speedup_ratio"]

            fft_output = {
                "frequencies": cut_spectrum["frequencies"],
                "sample_amplitude": cut_spectrum["sample_amplitude"],
                "sample_phase": cut_spectrum["sample_phase"],
                "reference_amplitude": cut_spectrum.get("reference_amplitude"),
                "reference_phase": cut_spectrum.get("reference_phase"),
                "band_info": band_info,
                "speedup_ratio": float(speedup_fft),
            }

            self.send_progress(
                analysis_id,
                "fft_done",
                40,
                f"FFT transformation completed, valid band: {band_info.get('start_freq_hz', 0)/1e12:.2f}-{band_info.get('end_freq_hz', 0)/1e12:.2f} THz",
                fft=fft_output,
            )

            anomaly_result = self.anomaly_detector.detect_anomaly(
                time=waveform["time"],
                field=waveform["sample_field"],
                frequencies=cut_spectrum["frequencies"],
                amplitude=cut_spectrum["sample_amplitude"],
                sample_thickness_mm=sample_thickness,
            )

            self.send_progress(
                analysis_id,
                "processing",
                45,
                "Extracting optical parameters",
            )

            perf_times["params_start"] = time.time()
            params_result = self.dorney.extract_parameters(
                frequencies=cut_spectrum["frequencies"],
                sample_amp=cut_spectrum["sample_amplitude"],
                sample_phase=cut_spectrum["sample_phase"],
                reference_amp=cut_spectrum.get("reference_amplitude"),
                reference_phase=cut_spectrum.get("reference_phase"),
                sample_thickness_mm=sample_thickness,
            )
            perf_times["params_end"] = time.time()

            params_output = {
                "frequencies": params_result["frequencies"],
                "absorption_coeff": params_result["absorption_coeff"],
                "refractive_index": params_result["refractive_index"],
                "extinction_coeff": params_result.get("extinction_coeff"),
                "band_info": band_info,
            }

            self.send_progress(
                analysis_id,
                "params_done",
                75,
                "Optical parameters extracted",
                params=params_output,
            )

            anomaly_result = self.anomaly_detector.detect_anomaly(
                time=waveform["time"],
                field=waveform["sample_field"],
                frequencies=params_result["frequencies"],
                amplitude=cut_spectrum["sample_amplitude"],
                alpha=params_result["absorption_coeff"],
                n=params_result["refractive_index"],
                sample_thickness_mm=sample_thickness,
            )

            if anomaly_result["is_invalid"]:
                print(f"Task {analysis_id}: Invalid sample detected - {anomaly_result['reasons']}")

                invalid_result = {
                    "analysis_id": analysis_id,
                    "status": "invalid",
                    "progress": 100,
                    "stage": "Invalid sample detected",
                    "anomaly_detection": {
                        "is_invalid": True,
                        "anomaly_score": anomaly_result["anomaly_score"],
                        "confidence": anomaly_result["confidence"],
                        "reasons": anomaly_result["reasons"],
                        "anomaly_type": anomaly_result["anomaly_type"],
                        "severity": anomaly_result["severity"],
                    },
                    "fft": fft_output,
                    "params": params_output,
                    "performance": {
                        "fft_time_ms": (perf_times["fft_end"] - perf_times["fft_start"]) * 1000,
                        "params_time_ms": (perf_times["params_end"] - perf_times["params_start"]) * 1000,
                        "total_time_ms": (time.time() - perf_times["total_start"]) * 1000,
                    },
                    "timestamp": datetime.utcnow().isoformat(),
                }
                self.send_result(invalid_result)
                return

            self.anomaly_detector.update_reference(anomaly_result["features"])
            self._valid_sample_count += 1

            self.send_progress(
                analysis_id,
                "processing",
                80,
                "Predicting moisture content",
            )

            perf_times["prediction_start"] = time.time()
            prediction = self.plsr_predictor.predict_with_details(
                frequencies=params_result["frequencies"],
                absorption_coeff=params_result["absorption_coeff"],
                refractive_index=params_result["refractive_index"],
            )
            perf_times["prediction_end"] = time.time()

            moisture = prediction["moisture_content"]
            total_speedup = prediction.get("speedup_ratio", 1.0) * speedup_fft

            self.send_progress(
                analysis_id,
                "completed",
                100,
                f"Analysis completed successfully, speedup: {total_speedup:.1f}x",
                moisture=moisture,
            )

            final_result = {
                "analysis_id": analysis_id,
                "status": "completed",
                "fft": fft_output,
                "params": params_output,
                "moisture_content_percent": moisture,
                "anomaly_detection": {
                    "is_invalid": False,
                    "anomaly_score": anomaly_result["anomaly_score"],
                    "confidence": anomaly_result["confidence"],
                    "anomaly_type": anomaly_result["anomaly_type"],
                    "severity": anomaly_result["severity"],
                },
                "performance": {
                    "fft_time_ms": (perf_times["fft_end"] - perf_times["fft_start"]) * 1000,
                    "params_time_ms": (perf_times["params_end"] - perf_times["params_start"]) * 1000,
                    "prediction_time_ms": (perf_times["prediction_end"] - perf_times["prediction_start"]) * 1000,
                    "total_time_ms": (time.time() - perf_times["total_start"]) * 1000,
                    "fft_speedup": float(speedup_fft),
                    "prediction_speedup": float(prediction.get("speedup_ratio", 1.0)),
                    "total_speedup": float(total_speedup),
                    "prediction_processing_time_ms": float(prediction.get("processing_time_ms", 0)),
                    "valid_samples_processed": self._valid_sample_count,
                },
            }

            if self.redis_client is not None:
                md5 = self.compute_waveform_md5(waveform["time"], waveform["sample_field"])
                cache_key = f"thz:cache:{md5}"
                cache_data = {
                    "analysis_id": analysis_id,
                    "data": final_result,
                    "cached_at": datetime.utcnow().isoformat(),
                    "hit_count": 0,
                }
                self.redis_client.setex(cache_key, config.CACHE_TTL_SECONDS, json.dumps(cache_data))

            self.send_result(final_result)

            print(f"Task {analysis_id} completed: moisture = {moisture:.4f}%, speedup = {total_speedup:.1f}x")

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

    def process_differential_task(self, task_data: Dict) -> None:
        comparison_id = task_data["id"]
        waveform_t1 = task_data["waveform_t1"]
        waveform_t2 = task_data["waveform_t2"]
        sample_thickness = task_data["sample_thickness_mm"]
        time_interval = task_data["time_interval_hours"]

        print(f"Processing differential task: {comparison_id}")

        try:
            result_t1 = self._process_single_waveform(waveform_t1, sample_thickness, f"{comparison_id}_t1")
            if result_t1["status"] in ["failed", "invalid"]:
                self._send_diff_result(comparison_id, "failed", error=f"T1 analysis {result_t1['status']}")
                return

            result_t2 = self._process_single_waveform(waveform_t2, sample_thickness, f"{comparison_id}_t2")
            if result_t2["status"] in ["failed", "invalid"]:
                self._send_diff_result(comparison_id, "failed", error=f"T2 analysis {result_t2['status']}")
                return

            moisture_t1 = result_t1["data"]["moisture_content_percent"]
            moisture_t2 = result_t2["data"]["moisture_content_percent"]

            freq_t1 = result_t1["data"]["params"]["frequencies"]
            alpha_t1 = result_t1["data"]["params"]["absorption_coeff"]
            n_t1 = result_t1["data"]["params"]["refractive_index"]
            freq_t2 = result_t2["data"]["params"]["frequencies"]
            alpha_t2 = result_t2["data"]["params"]["absorption_coeff"]
            n_t2 = result_t2["data"]["params"]["refractive_index"]

            from algorithms import TimePointData
            tp1 = TimePointData(
                time_hours=0.0,
                frequencies=freq_t1,
                absorption_coeff=alpha_t1,
                refractive_index=n_t1,
                moisture_content=moisture_t1,
            )
            tp2 = TimePointData(
                time_hours=time_interval,
                frequencies=freq_t2,
                absorption_coeff=alpha_t2,
                refractive_index=n_t2,
                moisture_content=moisture_t2,
            )

            diff_spectrum = self.differential_analyzer.compute_difference_spectrum(tp1, tp2)
            migration_rate = self.differential_analyzer.compute_moisture_migration_rate(tp1, tp2)
            drying_curve = self.differential_analyzer.analyze_drying_curve([tp1, tp2], degree=1)

            delta_moisture = moisture_t2 - moisture_t1
            is_drying = delta_moisture < 0

            diff_result = {
                "migration_rate_per_hour": migration_rate,
                "delta_moisture": delta_moisture,
                "moisture_t1": moisture_t1,
                "moisture_t2": moisture_t2,
                "difference_spectrum": {
                    "frequencies": diff_spectrum["frequencies"],
                    "delta_absorption": diff_spectrum["delta_absorption"].tolist(),
                    "absorption_ratio": diff_spectrum["absorption_ratio"].tolist(),
                    "delta_refractive_index": diff_spectrum.get("delta_refractive", []),
                    "mean_delta_alpha": float(diff_spectrum["mean_delta_alpha"]),
                    "max_delta_alpha": float(diff_spectrum["max_delta_alpha"]),
                    "integrated_delta": float(diff_spectrum["integrated_delta"]),
                },
                "drying_efficiency": float(drying_curve["efficiency"]) if drying_curve else 0.0,
                "half_life_hours": drying_curve.get("half_life_hours"),
                "is_drying": is_drying,
            }

            self._send_diff_result(comparison_id, "completed", result=diff_result)
            print(f"Differential task {comparison_id} completed: migration rate = {migration_rate:.4f}%/h")

        except Exception as e:
            print(f"Error processing differential task {comparison_id}: {e}")
            import traceback
            traceback.print_exc()
            self._send_diff_result(comparison_id, "failed", error=str(e))

    def _process_single_waveform(self, waveform, sample_thickness, analysis_id):
        try:
            perf_times = {"total_start": time.time()}

            anomaly_result = self.anomaly_detector.detect_anomaly(
                time=waveform["time"],
                field=waveform["sample_field"],
                sample_thickness_mm=sample_thickness,
            )
            if anomaly_result["is_invalid"]:
                return {"status": "invalid"}

            fft_result = self.fft_processor.process_waveform(
                time=waveform["time"],
                sample_field=waveform["sample_field"],
                reference_field=waveform.get("reference_field"),
            )

            cut_spectrum = self.band_cutter.cut_spectrum(
                frequencies=fft_result["frequencies"],
                sample_amp=fft_result["sample_amplitude"],
                sample_phase=fft_result["sample_phase"],
                reference_amp=fft_result.get("reference_amplitude"),
                reference_phase=fft_result.get("reference_phase"),
            )

            params_result = self.dorney.extract_parameters(
                frequencies=cut_spectrum["frequencies"],
                sample_amp=cut_spectrum["sample_amplitude"],
                sample_phase=cut_spectrum["sample_phase"],
                reference_amp=cut_spectrum.get("reference_amplitude"),
                reference_phase=cut_spectrum.get("reference_phase"),
                sample_thickness_mm=sample_thickness,
            )

            anomaly_result = self.anomaly_detector.detect_anomaly(
                time=waveform["time"],
                field=waveform["sample_field"],
                frequencies=params_result["frequencies"],
                amplitude=cut_spectrum["sample_amplitude"],
                alpha=params_result["absorption_coeff"],
                n=params_result["refractive_index"],
                sample_thickness_mm=sample_thickness,
            )
            if anomaly_result["is_invalid"]:
                return {"status": "invalid"}

            prediction = self.plsr_predictor.predict_with_details(
                frequencies=params_result["frequencies"],
                absorption_coeff=params_result["absorption_coeff"],
                refractive_index=params_result["refractive_index"],
            )

            return {
                "status": "completed",
                "data": {
                    "moisture_content_percent": prediction["moisture_content"],
                    "params": params_result,
                    "fft": fft_result,
                },
            }
        except Exception as e:
            return {"status": "failed", "error": str(e)}

    def _send_diff_result(self, comparison_id, status, error=None, result=None):
        msg = {
            "comparison_id": comparison_id,
            "status": status,
        }
        if error:
            msg["error"] = error
        if result:
            msg["result"] = result
        try:
            self.channel.basic_publish(
                exchange="",
                routing_key=config.DIFF_RESULT_QUEUE,
                body=json.dumps(msg),
                properties=pika.BasicProperties(delivery_mode=2, content_type="application/json"),
            )
        except Exception as e:
            print(f"Failed to send diff result: {e}")

    def callback(self, ch, method, properties, body):
        try:
            task_data = json.loads(body)
            print(f"Received task: {task_data.get('analysis_id')}")

            self.process_task(task_data)

            ch.basic_ack(delivery_tag=method.delivery_tag)

        except Exception as e:
            print(f"Error in callback: {e}")
            ch.basic_nack(delivery_tag=method.delivery_tag, requeue=False)

    def diff_callback(self, ch, method, properties, body):
        try:
            task_data = json.loads(body)
            print(f"Received differential task: {task_data.get('id')}")

            self.process_differential_task(task_data)

            ch.basic_ack(delivery_tag=method.delivery_tag)

        except Exception as e:
            print(f"Error in diff callback: {e}")
            ch.basic_nack(delivery_tag=method.delivery_tag, requeue=False)

    def start_consumer(self, enable_diff: bool = True):
        self.connect()
        self.running = True

        self.channel.basic_consume(
            queue=config.TASK_QUEUE,
            on_message_callback=self.callback,
            auto_ack=False,
        )

        if enable_diff:
            self.channel.basic_consume(
                queue=config.DIFF_TASK_QUEUE,
                on_message_callback=self.diff_callback,
                auto_ack=False,
            )

        print(f"Worker started, waiting for tasks on queue: {config.TASK_QUEUE}")
        if enable_diff:
            print(f"Also listening for differential tasks on: {config.DIFF_TASK_QUEUE}")
        print("Press Ctrl+C to stop")

        try:
            self.channel.start_consuming()
        except KeyboardInterrupt:
            print("Stopping worker...")
            self.disconnect()

    def start_multi_threaded(self, num_threads: int = 3, enable_diff: bool = True):
        self.running = True

        def worker_thread(thread_id):
            print(f"Starting worker thread {thread_id}")

            while self.running:
                try:
                    connection = pika.BlockingConnection(pika.URLParameters(config.RABBITMQ_URL))
                    channel = connection.channel()
                    channel.queue_declare(queue=config.TASK_QUEUE, durable=True)
                    if enable_diff:
                        channel.queue_declare(queue=config.DIFF_TASK_QUEUE, durable=True)
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

                    def diff_callback(ch, method, properties, body):
                        try:
                            task_data = json.loads(body)
                            print(f"Thread {thread_id}: Received diff task {task_data.get('id')}")
                            self.process_differential_task(task_data)
                            ch.basic_ack(delivery_tag=method.delivery_tag)
                        except Exception as e:
                            print(f"Thread {thread_id}: Error: {e}")
                            ch.basic_nack(delivery_tag=method.delivery_tag, requeue=False)

                    channel.basic_consume(
                        queue=config.TASK_QUEUE,
                        on_message_callback=callback,
                        auto_ack=False,
                    )
                    if enable_diff:
                        channel.basic_consume(
                            queue=config.DIFF_TASK_QUEUE,
                            on_message_callback=diff_callback,
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
