import numpy as np
from typing import Dict, List, Optional, Tuple


class SincResampler:
    STANDARD_FREQ_MIN_THZ = 0.1
    STANDARD_FREQ_MAX_THZ = 4.0
    STANDARD_FREQ_STEP_THZ = 0.01

    def __init__(
        self,
        freq_min_thz: float = 0.1,
        freq_max_thz: float = 4.0,
        freq_step_thz: float = 0.01,
        window_half_width: int = 16,
    ):
        self.freq_min_thz = freq_min_thz
        self.freq_max_thz = freq_max_thz
        self.freq_step_thz = freq_step_thz
        self.window_half_width = window_half_width

        self._standard_freqs_hz = np.arange(
            freq_min_thz * 1e12,
            (freq_max_thz + freq_step_thz * 0.5) * 1e12,
            freq_step_thz * 1e12,
        )
        self._standard_freqs_thz = self._standard_freqs_hz / 1e12
        self._num_points = len(self._standard_freqs_hz)

    @property
    def standard_freqs_hz(self) -> np.ndarray:
        return self._standard_freqs_hz.copy()

    @property
    def standard_freqs_thz(self) -> np.ndarray:
        return self._standard_freqs_thz.copy()

    @property
    def num_standard_points(self) -> int:
        return self._num_points

    def _sinc_interp(
        self,
        x_old: np.ndarray,
        y_old: np.ndarray,
        x_new: np.ndarray,
    ) -> np.ndarray:
        if len(x_old) < 2:
            return np.zeros_like(x_new)

        dt_old = np.median(np.diff(x_old))
        if dt_old <= 0:
            return np.interp(x_new, x_old, y_old, left=0.0, right=0.0)

        result = np.zeros(len(x_new), dtype=np.float64)

        for i, xn in enumerate(x_new):
            left_idx = np.searchsorted(x_old, xn - self.window_half_width * dt_old)
            right_idx = np.searchsorted(x_old, xn + self.window_half_width * dt_old)
            left_idx = max(0, left_idx)
            right_idx = min(len(x_old), right_idx)

            if left_idx >= right_idx:
                if len(x_old) > 0:
                    nearest = np.argmin(np.abs(x_old - xn))
                    result[i] = y_old[nearest]
                continue

            x_window = x_old[left_idx:right_idx]
            y_window = y_old[left_idx:right_idx]

            t = (xn - x_window) / dt_old
            sinc_vals = np.sinc(t)
            weights = sinc_vals * dt_old

            weight_sum = np.sum(np.abs(weights))
            if weight_sum > 1e-12:
                result[i] = np.sum(y_window * weights)
            else:
                nearest = np.argmin(np.abs(x_window - xn))
                result[i] = y_window[nearest]

        return result

    def resample_spectrum(
        self,
        frequencies: np.ndarray,
        values: np.ndarray,
    ) -> Tuple[np.ndarray, Dict]:
        if len(frequencies) == 0 or len(values) == 0:
            return (
                np.zeros(self._num_points),
                {"resampled": False, "reason": "empty_input", "original_len": 0},
            )

        mask = np.isfinite(values) & np.isfinite(frequencies)
        if not np.any(mask):
            return (
                np.zeros(self._num_points),
                {"resampled": False, "reason": "no_valid_data", "original_len": len(values)},
            )

        freq_clean = frequencies[mask]
        val_clean = values[mask]

        sort_idx = np.argsort(freq_clean)
        freq_sorted = freq_clean[sort_idx]
        val_sorted = val_clean[sort_idx]

        freq_min_input = freq_sorted[0]
        freq_max_input = freq_sorted[-1]
        target_min = self._standard_freqs_hz[0]
        target_max = self._standard_freqs_hz[-1]

        if freq_max_input <= target_min or freq_min_input >= target_max:
            return (
                np.zeros(self._num_points),
                {
                    "resampled": False,
                    "reason": "no_overlap",
                    "original_len": len(values),
                    "input_range_thz": [freq_min_input / 1e12, freq_max_input / 1e12],
                    "target_range_thz": [target_min / 1e12, target_max / 1e12],
                },
            )

        resampled = self._sinc_interp(freq_sorted, val_sorted, self._standard_freqs_hz)

        out_of_range_mask = (self._standard_freqs_hz < freq_min_input) | (
            self._standard_freqs_hz > freq_max_input
        )
        resampled[out_of_range_mask] = 0.0

        return resampled, {
            "resampled": True,
            "original_len": len(values),
            "resampled_len": self._num_points,
            "input_freq_range_thz": [freq_min_input / 1e12, freq_max_input / 1e12],
            "output_freq_range_thz": [self.freq_min_thz, self.freq_max_thz],
            "freq_step_thz": self.freq_step_thz,
            "num_valid_points": int(np.sum(~out_of_range_mask)),
            "interpolation_method": "sinc",
        }

    def resample_optical_params(
        self,
        frequencies: List[float],
        absorption_coeff: List[float],
        refractive_index: List[float],
    ) -> Dict:
        freq_arr = np.array(frequencies, dtype=np.float64)
        alpha_arr = np.array(absorption_coeff, dtype=np.float64)
        n_arr = np.array(refractive_index, dtype=np.float64)

        alpha_resampled, alpha_info = self.resample_spectrum(freq_arr, alpha_arr)
        n_resampled, n_info = self.resample_spectrum(freq_arr, n_arr)

        return {
            "frequencies": self._standard_freqs_hz.tolist(),
            "frequencies_thz": self._standard_freqs_thz.tolist(),
            "absorption_coeff": alpha_resampled.tolist(),
            "refractive_index": n_resampled.tolist(),
            "resample_info": {
                "alpha": alpha_info,
                "refractive_index": n_info,
                "standard_freq_min_thz": self.freq_min_thz,
                "standard_freq_max_thz": self.freq_max_thz,
                "standard_freq_step_thz": self.freq_step_thz,
                "standard_num_points": self._num_points,
            },
        }

    def resample_spectrum_data(
        self,
        frequencies: List[float],
        amplitude: List[float],
        phase: List[float],
    ) -> Dict:
        freq_arr = np.array(frequencies, dtype=np.float64)
        amp_arr = np.array(amplitude, dtype=np.float64)
        phase_arr = np.array(phase, dtype=np.float64)

        amp_resampled, amp_info = self.resample_spectrum(freq_arr, amp_arr)
        phase_resampled, phase_info = self.resample_spectrum(freq_arr, phase_arr)

        return {
            "frequencies": self._standard_freqs_hz.tolist(),
            "frequencies_thz": self._standard_freqs_thz.tolist(),
            "amplitude": amp_resampled.tolist(),
            "phase": phase_resampled.tolist(),
            "resample_info": {
                "amplitude": amp_info,
                "phase": phase_info,
            },
        }

    def check_needs_resampling(self, frequencies: List[float]) -> bool:
        if len(frequencies) != self._num_points:
            return True

        freq_arr = np.array(frequencies, dtype=np.float64)
        if len(freq_arr) < 2:
            return True

        dt = np.median(np.diff(freq_arr))
        expected_dt = self.freq_step_thz * 1e12
        if abs(dt - expected_dt) / expected_dt > 0.01:
            return True

        if abs(freq_arr[0] - self._standard_freqs_hz[0]) / self._standard_freqs_hz[0] > 0.01:
            return True

        return False
