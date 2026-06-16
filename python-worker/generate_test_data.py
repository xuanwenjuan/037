import os
import numpy as np
import json
import csv


def generate_thz_waveform(
    n_points: int = 1024,
    time_range_ps: float = 100.0,
    moisture_content: float = 5.0,
    include_reference: bool = True,
    noise_level: float = 0.02,
):
    time_ps = np.linspace(0, time_range_ps, n_points)
    time_s = time_ps * 1e-12

    t0 = 30e-12
    sigma = 0.3e-12
    omega0 = 2 * np.pi * 1e12

    reference_env = np.exp(-((time_s - t0) ** 2) / (2 * sigma ** 2))
    reference_field = reference_env * np.cos(omega0 * (time_s - t0))
    reference_field += noise_level * np.random.randn(n_points)

    sample_t0 = t0 + 0.15e-12 * (1 + moisture_content / 100)
    sample_sigma = sigma * (1 + 0.05 * moisture_content)

    alpha_factor = 1.0 + 0.3 * moisture_content

    sample_env = (1.0 / alpha_factor) * np.exp(-((time_s - sample_t0) ** 2) / (2 * sample_sigma ** 2))
    sample_env *= np.exp(-2.0 * moisture_content * (time_s - sample_t0) * (time_s > sample_t0))

    sample_field = sample_env * np.cos(omega0 * (time_s - sample_t0))
    sample_field += noise_level * np.random.randn(n_points)

    result = {
        "time": time_ps.tolist(),
        "sample_field": sample_field.tolist(),
    }

    if include_reference:
        result["reference_field"] = reference_field.tolist()

    return result


def save_json(data, output_path):
    os.makedirs(os.path.dirname(output_path), exist_ok=True)
    with open(output_path, "w") as f:
        json.dump(data, f, indent=2)
    print(f"Saved JSON: {output_path}")


def save_csv(data, output_path):
    os.makedirs(os.path.dirname(output_path), exist_ok=True)

    fieldnames = ["time", "sample_field"]
    if "reference_field" in data:
        fieldnames.append("reference_field")

    with open(output_path, "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()

        n = len(data["time"])
        for i in range(n):
            row = {
                "time": data["time"][i],
                "sample_field": data["sample_field"][i],
            }
            if "reference_field" in data:
                row["reference_field"] = data["reference_field"][i]
            writer.writerow(row)

    print(f"Saved CSV: {output_path}")


def main():
    import argparse
    parser = argparse.ArgumentParser(description="Generate test THz waveform data")
    parser.add_argument(
        "--output-dir",
        type=str,
        default="./test_data",
        help="Output directory for test data",
    )
    parser.add_argument(
        "--n-samples",
        type=int,
        default=5,
        help="Number of test samples to generate",
    )
    parser.add_argument(
        "--n-points",
        type=int,
        default=1024,
        help="Number of data points per waveform",
    )
    parser.add_argument(
        "--format",
        type=str,
        choices=["json", "csv", "both"],
        default="both",
        help="Output format",
    )
    args = parser.parse_args()

    moisture_values = np.linspace(1.0, 25.0, args.n_samples)

    print(f"Generating {args.n_samples} test samples...")
    print(f"Moisture range: {moisture_values[0]:.1f}% to {moisture_values[-1]:.1f}%")
    print(f"Data points per sample: {args.n_points}")
    print()

    for i, moisture in enumerate(moisture_values):
        sample_name = f"sample_{i+1:02d}_moisture_{moisture:.1f}pct"

        data = generate_thz_waveform(
            n_points=args.n_points,
            moisture_content=moisture,
            include_reference=True,
        )

        if args.format in ["json", "both"]:
            json_path = os.path.join(args.output_dir, f"{sample_name}.json")
            save_json(data, json_path)

        if args.format in ["csv", "both"]:
            csv_path = os.path.join(args.output_dir, f"{sample_name}.csv")
            save_csv(data, csv_path)

    print()
    print("Test data generation complete!")
    print(f"Output directory: {os.path.abspath(args.output_dir)}")


if __name__ == "__main__":
    main()
