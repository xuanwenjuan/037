import os
import sys
import json
import time
import requests
import websocket
import threading
from generate_test_data import generate_thz_waveform, save_json, save_csv


BASE_URL = "http://localhost:8080"
WS_URL = "ws://localhost:8080"


def test_health():
    print("Testing health check...")
    try:
        response = requests.get(f"{BASE_URL}/api/v1/health")
        print(f"  Status: {response.status_code}")
        print(f"  Response: {json.dumps(response.json(), indent=2)}")
        return response.status_code == 200
    except Exception as e:
        print(f"  Error: {e}")
        return False


def test_upload(file_path, sample_name, thickness, file_type="json"):
    print(f"\nTesting upload with {file_type} file: {sample_name}")
    try:
        with open(file_path, "rb") as f:
            files = {"file": (os.path.basename(file_path), f, f"application/{file_type}")}
            data = {
                "sample_name": sample_name,
                "material_type": "grain",
                "sample_thickness_mm": str(thickness),
            }
            response = requests.post(
                f"{BASE_URL}/api/v1/analyses/upload",
                files=files,
                data=data,
            )
        print(f"  Status: {response.status_code}")
        result = response.json()
        print(f"  Response: {json.dumps(result, indent=2)}")
        return result.get("analysis_id") if response.status_code == 202 else None
    except Exception as e:
        print(f"  Error: {e}")
        return None


def test_list_analyses():
    print("\nTesting list analyses...")
    try:
        response = requests.get(f"{BASE_URL}/api/v1/analyses?limit=10")
        print(f"  Status: {response.status_code}")
        result = response.json()
        print(f"  Total: {result.get('total')}")
        print(f"  Count: {len(result.get('data', []))}")
        return result
    except Exception as e:
        print(f"  Error: {e}")
        return None


def test_get_analysis(analysis_id):
    print(f"\nTesting get analysis: {analysis_id}")
    try:
        response = requests.get(f"{BASE_URL}/api/v1/analyses/{analysis_id}")
        print(f"  Status: {response.status_code}")
        result = response.json()
        print(f"  Status: {result.get('analysis', {}).get('status')}")
        if result.get("analysis", {}).get("moisture_content_percent") is not None:
            print(f"  Moisture: {result['analysis']['moisture_content_percent']:.4f}%")
        return result
    except Exception as e:
        print(f"  Error: {e}")
        return None


def test_websocket(analysis_id, timeout=30):
    print(f"\nTesting WebSocket for analysis: {analysis_id}")
    messages = []
    done = threading.Event()

    def on_message(ws, message):
        data = json.loads(message)
        print(f"  WS Message [{data.get('progress')}%]: {data.get('status')} - {data.get('message')}")
        messages.append(data)
        if data.get("status") in ["completed", "failed"]:
            done.set()

    def on_error(ws, error):
        print(f"  WS Error: {error}")

    def on_open(ws):
        print(f"  WS Connected")

    def on_close(ws, close_status_code, close_msg):
        print(f"  WS Closed: {close_status_code}")
        done.set()

    ws = websocket.WebSocketApp(
        f"{WS_URL}/api/v1/analyses/{analysis_id}/ws",
        on_message=on_message,
        on_error=on_error,
        on_open=on_open,
        on_close=on_close,
    )

    wst = threading.Thread(target=ws.run_forever)
    wst.daemon = True
    wst.start()

    done.wait(timeout=timeout)
    ws.close()

    return messages


def main():
    import argparse
    parser = argparse.ArgumentParser(description="Test THz Analysis API")
    parser.add_argument("--skip-ws", action="store_true", help="Skip WebSocket test")
    parser.add_argument("--gen-test-data", action="store_true", help="Generate test data first")
    args = parser.parse_args()

    test_dir = "./test_data"

    if args.gen_test_data:
        print("Generating test data...")
        os.makedirs(test_dir, exist_ok=True)
        data1 = generate_thz_waveform(moisture_content=5.0, include_reference=True)
        save_json(data1, os.path.join(test_dir, "test_sample_5pct.json"))
        save_csv(data1, os.path.join(test_dir, "test_sample_5pct.csv"))

        data2 = generate_thz_waveform(moisture_content=15.0, include_reference=True)
        save_json(data2, os.path.join(test_dir, "test_sample_15pct.json"))

        data3 = generate_thz_waveform(moisture_content=25.0, include_reference=False)
        save_json(data3, os.path.join(test_dir, "test_sample_25pct_no_ref.json"))
        print()

    print("=" * 60)
    print("THz Analysis API Test Suite")
    print("=" * 60)

    if not test_health():
        print("\nServer is not running. Please start the server first.")
        return

    analysis_ids = []

    json_file = os.path.join(test_dir, "test_sample_5pct.json")
    if os.path.exists(json_file):
        aid = test_upload(json_file, "Test Wheat 5%", 2.5, "json")
        if aid:
            analysis_ids.append(aid)

    csv_file = os.path.join(test_dir, "test_sample_5pct.csv")
    if os.path.exists(csv_file):
        aid = test_upload(csv_file, "Test Wheat 5% CSV", 2.5, "csv")
        if aid:
            analysis_ids.append(aid)

    json_file2 = os.path.join(test_dir, "test_sample_15pct.json")
    if os.path.exists(json_file2):
        aid = test_upload(json_file2, "Test Corn 15%", 3.0, "json")
        if aid:
            analysis_ids.append(aid)

    test_list_analyses()

    if analysis_ids and not args.skip_ws:
        print(f"\n{'=' * 60}")
        print(f"Testing WebSocket with analysis: {analysis_ids[0]}")
        print("=" * 60)
        test_websocket(analysis_ids[0], timeout=30)

    if analysis_ids:
        print(f"\n{'=' * 60}")
        print("Checking final results...")
        print("=" * 60)

        for aid in analysis_ids:
            max_attempts = 10
            for i in range(max_attempts):
                result = test_get_analysis(aid)
                status = result.get("analysis", {}).get("status") if result else None
                if status in ["completed", "failed"]:
                    break
                if i < max_attempts - 1:
                    print(f"  Waiting for completion... ({i+1}/{max_attempts})")
                    time.sleep(2)

    print("\n" + "=" * 60)
    print("Test suite complete!")
    print("=" * 60)

    print("\nAPI Endpoints Summary:")
    print("  POST   /api/v1/analyses/upload      - Upload waveform file")
    print("  GET    /api/v1/analyses              - List analyses")
    print("  GET    /api/v1/analyses/:id          - Get analysis details")
    print("  GET    /api/v1/analyses/:id/ws       - WebSocket progress")
    print("  GET    /api/v1/health                - Health check")


if __name__ == "__main__":
    main()
