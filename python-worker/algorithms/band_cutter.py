import numpy as np
from typing import Dict, List, Tuple, Optional


class BandCutter:
    def __init__(
        self,
        min_freq_thz: float = 0.1,
        max_freq_thz: float = 4.0,
        snr_threshold: float = 3.0,
        smooth_window: int = 5,
    ):
        self.min_freq_thz = min_freq_thz
        self.max_freq_thz = max_freq_thz
        self.snr_threshold = snr_threshold
        self.smooth_window = smooth_window

    def compute_snr(
        self, frequencies: np.ndarray, amplitude: np.ndarray
    ) -> np.ndarray:
        if len(amplitude) < self.smooth_window * 3:
            return np.ones_like(amplitude)

        smoothed = self._moving_average(amplitude, self.smooth_window)
        noise = np.abs(amplitude - smoothed)

        noise_level = np.median(noise[noise > 0]) if np.any(noise > 0) else 1e-10
        noise_level = max(noise_level, 1e-10)

        snr = smoothed / noise_level
        return snr

    def _moving_average(self, x: np.ndarray, window: int) -> np.ndarray:
        if len(x) < window:
            return x
        kernel = np.ones(window) / window
        return np.convolve(x, kernel, mode="same")

    def find_valid_band(
        self,
        frequencies: List[float],
        amplitude: List[float],
        reference_amplitude: Optional[List[float]] = None,
    ) -> Dict:
        freq_arr = np.array(frequencies, dtype=np.float64)
        amp_arr = np.array(amplitude, dtype=np.float64)

        min_freq_hz = self.min_freq_thz * 1e12
        max_freq_hz = self.max_freq_thz * 1e12

        basic_mask = (freq_arr >= min_freq_hz) & (freq_arr <= max_freq_hz)

        if reference_amplitude is not None and len(reference_amplitude) == len(
            amplitude
        ):
            ref_arr = np.array(reference_amplitude, dtype=np.float64)
            snr = self.compute_snr(freq_arr, ref_arr)
        else:
            snr = self.compute_snr(freq_arr, amp_arr)

        snr_mask = snr >= self.snr_threshold

        valid_mask = basic_mask & snr_mask

        if not np.any(valid_mask):
            valid_indices = np.where(basic_mask)[0]
            if len(valid_indices) == 0:
                return {
                    "valid": False,
                    "start_freq_hz": 0.0,
                    "end_freq_hz": 0.0,
                    "start_index": 0,
                    "end_index": 0,
                    "num_points": 0,
                    "snr_mean": 0.0,
                    "snr_max": 0.0,
                }
            start_idx = valid_indices[0]
            end_idx = valid_indices[-1]
        else:
            valid_indices = np.where(valid_mask)[0]
            start_idx = valid_indices[0]
            end_idx = valid_indices[-1]

        return {
            "valid": True,
            "start_freq_hz": float(freq_arr[start_idx]),
            "end_freq_hz": float(freq_arr[end_idx]),
            "start_index": int(start_idx),
            "end_index": int(end_idx),
            "num_points": int(end_idx - start_idx + 1),
            "snr_mean": float(np.mean(snr[start_idx : end_idx + 1])),
            "snr_max": float(np.max(snr[start_idx : end_idx + 1])),
        }

    def cut_spectrum(
        self,
        frequencies: List[float],
        sample_amp: List[float],
        sample_phase: List[float],
        reference_amp: Optional[List[float]] = None,
        reference_phase: Optional[List[float]] = None,
    ) -> Dict:
        band_info = self.find_valid_band(
            frequencies, sample_amp, reference_amp
        )

        if not band_info["valid"]:
            return {
                "frequencies": [],
                "sample_amplitude": [],
                "sample_phase": [],
                "reference_amplitude": None if reference_amp else [],
                "reference_phase": None if reference_phase else [],
                "band_info": band_info,
                "speedup_ratio": 1.0,
            }

        start = band_info["start_index"]
        end = band_info["end_index"]

        result = {
            "frequencies": frequencies[start : end + 1],
            "sample_amplitude": sample_amp[start : end + 1],
            "sample_phase": sample_phase[start : end + 1],
            "band_info": band_info,
            "speedup_ratio": len(frequencies) / band_info["num_points"],
        }

        if reference_amp is not None and len(reference_amp) == len(frequencies):
            result["reference_amplitude"] = reference_amp[start : end + 1]
        if reference_phase is not None and len(reference_phase) == len(frequencies):
            result["reference_phase"] = reference_phase[start : end + 1]

        return result

    def cut_params(
        self,
        frequencies: List[float],
        absorption_coeff: List[float],
        refractive_index: List[float],
        extinction_coeff: Optional[List[float]] = None,
    ) -> Dict:
        freq_arr = np.array(frequencies, dtype=np.float64)
        alpha_arr = np.array(absorption_coeff, dtype=np.float64)

        min_freq_hz = self.min_freq_thz * 1e12
        max_freq_hz = self.max_freq_thz * 1e12

        mask = (freq_arr >= min_freq_hz) & (freq_arr <= max_freq_hz)

        if not np.any(mask):
            return {
                "frequencies": [],
                "absorption_coeff": [],
                "refractive_index": [],
                "extinction_coeff": None,
                "band_info": {
                    "valid": False,
                    "start_freq_hz": 0.0,
                    "end_freq_hz": 0.0,
                    "num_points": 0,
                },
                "speedup_ratio": 1.0,
            }

        indices = np.where(mask)[0]
        start_idx, end_idx = indices[0], indices[-1]

        band_info = {
            "valid": True,
            "start_freq_hz": float(freq_arr[start_idx]),
            "end_freq_hz": float(freq_arr[end_idx]),
            "start_index": int(start_idx),
            "end_index": int(end_idx),
            "num_points": int(end_idx - start_idx + 1),
        }

        result = {
            "frequencies": frequencies[start_idx : end_idx + 1],
            "absorption_coeff": absorption_coeff[start_idx : end_idx + 1],
            "refractive_index": refractive_index[start_idx : end_idx + 1],
            "band_info": band_info,
            "speedup_ratio": len(frequencies) / band_info["num_points"],
        }

        if extinction_coeff is not None and len(extinction_coeff) == len(frequencies):
            result["extinction_coeff"] = extinction_coeff[start_idx : end_idx + 1]

        return result

    def select_sensitive_bands(
        self,
        frequencies: List[float],
        absorption_coeff: List[float],
        refractive_index: List[float],
        num_bands: int = 5,
        band_width_thz: float = 0.2,
    ) -> List[Dict]:
        freq_arr = np.array(frequencies, dtype=np.float64)
        alpha_arr = np.array(absorption_coeff, dtype=np.float64)
        n_arr = np.array(refractive_index, dtype=np.float64)

        min_freq_hz = self.min_freq_thz * 1e12
        max_freq_hz = self.max_freq_thz * 1e12

        valid_mask = (freq_arr >= min_freq_hz) & (freq_arr <= max_freq_hz)

        if not np.any(valid_mask):
            return []

        valid_freq = freq_arr[valid_mask]
        valid_alpha = alpha_arr[valid_mask]

        alpha_smooth = self._moving_average(valid_alpha, self.smooth_window)
        alpha_deriv = np.gradient(alpha_smooth, valid_freq)

        sensitivity = np.abs(alpha_deriv) + 0.1 * alpha_smooth

        peaks = self._find_peaks(sensitivity)

        if len(peaks) == 0:
            return []

        peak_indices = np.argsort(sensitivity[peaks])[::-1][:num_bands]
        selected_peaks = [peaks[i] for i in peak_indices]

        bands = []
        band_width_hz = band_width_thz * 1e12

        for peak_idx in selected_peaks:
            center_freq = valid_freq[peak_idx]
            start_freq = max(center_freq - band_width_hz / 2, min_freq_hz)
            end_freq = min(center_freq + band_width_hz / 2, max_freq_hz)

            band_mask = (valid_freq >= start_freq) & (valid_freq <= end_freq)
            band_indices = np.where(valid_mask)[0][band_mask]

            if len(band_indices) > 0:
                bands.append(
                    {
                        "center_freq_hz": float(center_freq),
                        "start_freq_hz": float(start_freq),
                        "end_freq_hz": float(end_freq),
                        "num_points": int(np.sum(band_mask)),
                        "mean_alpha": float(np.mean(valid_alpha[band_mask])),
                        "sensitivity": float(sensitivity[peak_idx]),
                        "start_index": int(band_indices[0]),
                        "end_index": int(band_indices[-1]),
                    }
                )

        bands.sort(key=lambda x: x["center_freq_hz"])
        return bands

    def _find_peaks(self, x: np.ndarray, min_distance: int = 10) -> List[int]:
        if len(x) < 3:
            return []

        peaks = []
        for i in range(1, len(x) - 1):
            if x[i] > x[i - 1] and x[i] > x[i + 1]:
                if not peaks or i - peaks[-1] >= min_distance:
                    peaks.append(i)
                elif x[i] > x[peaks[-1]]:
                    peaks[-1] = i

        return peaks
