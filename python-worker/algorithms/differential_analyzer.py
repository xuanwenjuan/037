import numpy as np
from typing import Dict, List, Optional, Tuple
from dataclasses import dataclass


@dataclass
class TimePointData:
    time_hours: float
    frequencies: List[float]
    absorption_coeff: List[float]
    refractive_index: List[float]
    moisture_content: float


class DifferentialAnalyzer:
    def __init__(self):
        pass

    def compute_difference_spectrum(
        self,
        frequencies: List[float],
        alpha_t1: List[float],
        alpha_t2: List[float],
        n_t1: Optional[List[float]] = None,
        n_t2: Optional[List[float]] = None,
    ) -> Dict:
        freq_arr = np.array(frequencies, dtype=np.float64)
        alpha1 = np.array(alpha_t1, dtype=np.float64)
        alpha2 = np.array(alpha_t2, dtype=np.float64)

        if len(alpha1) != len(alpha2) or len(alpha1) != len(freq_arr):
            raise ValueError("All arrays must have the same length")

        delta_alpha = alpha2 - alpha1
        alpha_ratio = np.divide(
            delta_alpha,
            alpha1 + 1e-10,
            out=np.zeros_like(alpha1),
            where=alpha1 != 0,
        )

        result = {
            "frequencies": freq_arr.tolist(),
            "delta_absorption": delta_alpha.tolist(),
            "absorption_ratio": alpha_ratio.tolist(),
            "mean_delta_alpha": float(np.mean(delta_alpha)),
            "max_delta_alpha": float(np.max(delta_alpha)),
            "min_delta_alpha": float(np.min(delta_alpha)),
            "integrated_delta": float(np.trapz(delta_alpha, freq_arr)),
        }

        if n_t1 is not None and n_t2 is not None:
            n1 = np.array(n_t1, dtype=np.float64)
            n2 = np.array(n_t2, dtype=np.float64)
            delta_n = n2 - n1
            result["delta_refractive_index"] = delta_n.tolist()
            result["mean_delta_n"] = float(np.mean(delta_n))

        return result

    def compute_moisture_migration_rate(
        self,
        moisture_t1: float,
        moisture_t2: float,
        time_interval_hours: float,
    ) -> Dict:
        if time_interval_hours <= 0:
            raise ValueError("Time interval must be positive")

        delta_moisture = moisture_t2 - moisture_t1
        rate = delta_moisture / time_interval_hours
        relative_rate = (delta_moisture / (moisture_t1 + 1e-10)) * 100

        return {
            "initial_moisture": moisture_t1,
            "final_moisture": moisture_t2,
            "delta_moisture": delta_moisture,
            "time_interval_hours": time_interval_hours,
            "migration_rate_per_hour": rate,
            "relative_rate_percent_per_hour": relative_rate,
            "is_drying": delta_moisture < 0,
            "is_hydrating": delta_moisture > 0,
        }

    def analyze_drying_curve(
        self,
        time_points: List[TimePointData],
        reference_freq_thz: float = 1.0,
    ) -> Dict:
        if len(time_points) < 2:
            raise ValueError("At least 2 time points are required")

        sorted_points = sorted(time_points, key=lambda x: x.time_hours)

        times = np.array([p.time_hours for p in sorted_points], dtype=np.float64)
        moistures = np.array([p.moisture_content for p in sorted_points], dtype=np.float64)

        ref_freq_hz = reference_freq_thz * 1e12
        alpha_at_ref = []

        for point in sorted_points:
            freq_arr = np.array(point.frequencies)
            alpha_arr = np.array(point.absorption_coeff)
            alpha_interp = np.interp(ref_freq_hz, freq_arr, alpha_arr)
            alpha_at_ref.append(alpha_interp)

        alpha_at_ref = np.array(alpha_at_ref, dtype=np.float64)

        rates = []
        for i in range(len(sorted_points) - 1):
            dt = times[i + 1] - times[i]
            if dt > 0:
                rate = (moistures[i + 1] - moistures[i]) / dt
                rates.append(
                    {
                        "time_interval_start_hours": float(times[i]),
                        "time_interval_end_hours": float(times[i + 1]),
                        "migration_rate_per_hour": float(rate),
                        "delta_moisture": float(moistures[i + 1] - moistures[i]),
                    }
                )

        if len(times) >= 3:
            coeffs = np.polyfit(times, moistures, 2)
            a, b, c = coeffs
            critical_time = -b / (2 * a) if a != 0 else None
            predicted_moisture_at_critical = (
                a * critical_time ** 2 + b * critical_time + c
                if critical_time is not None
                else None
            )
        else:
            coeffs = np.polyfit(times, moistures, 1)
            a, b = coeffs
            critical_time = None
            predicted_moisture_at_critical = None

        overall_rate = (moistures[-1] - moistures[0]) / (times[-1] - times[0]) if times[-1] != times[0] else 0

        alpha_trend = np.polyfit(times, alpha_at_ref, 1)[0] if len(times) >= 2 else 0

        return {
            "time_points": [
                {
                    "time_hours": float(p.time_hours),
                    "moisture_content": float(p.moisture_content),
                    "alpha_at_ref": float(alpha_at_ref[i]),
                }
                for i, p in enumerate(sorted_points)
            ],
            "interval_rates": rates,
            "overall_migration_rate": float(overall_rate),
            "moisture_time_coeffs": coeffs.tolist(),
            "critical_time_hours": float(critical_time) if critical_time is not None else None,
            "predicted_moisture_at_critical": float(predicted_moisture_at_critical)
            if predicted_moisture_at_critical is not None
            else None,
            "alpha_trend": float(alpha_trend),
            "total_moisture_loss": float(moistures[0] - moistures[-1]),
            "drying_efficiency": float((moistures[0] - moistures[-1]) / (moistures[0] + 1e-10) * 100),
            "half_life_hours": self._compute_half_life(times, moistures),
        }

    def _compute_half_life(
        self, times: np.ndarray, moistures: np.ndarray
    ) -> Optional[float]:
        if len(times) < 2:
            return None

        initial_moisture = moistures[0]
        target_moisture = initial_moisture / 2

        for i in range(len(moistures) - 1):
            if (moistures[i] - target_moisture) * (moistures[i + 1] - target_moisture) <= 0:
                t1, t2 = times[i], times[i + 1]
                m1, m2 = moistures[i], moistures[i + 1]
                if abs(m2 - m1) > 1e-10:
                    half_time = t1 + (target_moisture - m1) * (t2 - t1) / (m2 - m1)
                    return float(half_time)

        return None

    def compute_spatial_migration_map(
        self,
        frequencies: List[float],
        alpha_t1: List[float],
        alpha_t2: List[float],
        sample_thickness_mm: float,
        num_layers: int = 5,
    ) -> Dict:
        freq_arr = np.array(frequencies, dtype=np.float64)
        alpha1 = np.array(alpha_t1, dtype=np.float64)
        alpha2 = np.array(alpha_t2, dtype=np.float64)
        delta_alpha = alpha2 - alpha1

        thickness_m = sample_thickness_mm * 1e-3
        layers = np.linspace(0, thickness_m, num_layers)

        migration_map = []
        for layer_pos in layers:
            depth_factor = np.exp(-freq_arr * layer_pos / 3e8)
            weighted_delta = delta_alpha * depth_factor
            migration_strength = float(np.trapz(weighted_delta, freq_arr))

            migration_map.append(
                {
                    "depth_mm": float(layer_pos * 1000),
                    "migration_strength": migration_strength,
                    "relative_strength": migration_strength
                    / (np.max(np.abs(delta_alpha)) + 1e-10),
                }
            )

        return {
            "sample_thickness_mm": sample_thickness_mm,
            "num_layers": num_layers,
            "migration_map": migration_map,
            "surface_migration": migration_map[0]["migration_strength"]
            if migration_map
            else 0,
            "bulk_migration": migration_map[len(migration_map) // 2]["migration_strength"]
            if migration_map
            else 0,
            "migration_gradient": (migration_map[0]["migration_strength"] - migration_map[-1]["migration_strength"])
            / sample_thickness_mm
            if len(migration_map) >= 2
            else 0,
        }

    def compare_multiple_samples(
        self,
        sample_results: List[Dict],
    ) -> Dict:
        names = [r.get("sample_name", f"Sample_{i}") for i, r in enumerate(sample_results)]
        moistures = [r.get("moisture_content", 0) for r in sample_results]
        mean_alpha = []
        for r in sample_results:
            params = r.get("optical_params", {})
            alpha = params.get("absorption_coeff", [])
            mean_alpha.append(float(np.mean(alpha)) if alpha else 0)

        best_idx = int(np.argmin(moistures))
        worst_idx = int(np.argmax(moistures))

        return {
            "sample_count": len(sample_results),
            "sample_names": names,
            "moisture_contents": moistures,
            "mean_absorption_coeff": mean_alpha,
            "moisture_range": float(np.max(moistures) - np.min(moistures)),
            "moisture_std": float(np.std(moistures)),
            "driest_sample": names[best_idx],
            "wettest_sample": names[worst_idx],
            "rankings": sorted(
                [{"name": n, "moisture": m, "rank": i + 1}
                 for i, (n, m) in enumerate(
                     sorted(zip(names, moistures), key=lambda x: x[1])
                 )],
                key=lambda x: x["name"]
            ),
        }
