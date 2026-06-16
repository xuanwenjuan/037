import numpy as np
from typing import Dict, List, Optional


class DorneyDuvillaret:
    def __init__(self, c: float = 299792458.0):
        self.c = c

    def extract_parameters(
        self,
        frequencies: List[float],
        sample_amp: List[float],
        sample_phase: List[float],
        reference_amp: Optional[List[float]],
        reference_phase: Optional[List[float]],
        sample_thickness_mm: float,
        sample_phase_unwrapped: Optional[List[float]] = None,
        reference_phase_unwrapped: Optional[List[float]] = None,
    ) -> Dict[str, List[float]]:
        d = sample_thickness_mm * 1e-3

        freq_arr = np.array(frequencies, dtype=np.float64)
        samp_amp = np.array(sample_amp, dtype=np.float64)
        samp_phase = np.array(sample_phase, dtype=np.float64)

        if reference_amp is None or reference_phase is None:
            return self._extract_without_reference(
                freq_arr, samp_amp, samp_phase, d
            )

        ref_amp = np.array(reference_amp, dtype=np.float64)
        ref_phase = np.array(reference_phase, dtype=np.float64)

        return self._extract_with_reference(
            freq_arr,
            samp_amp,
            samp_phase,
            ref_amp,
            ref_phase,
            d,
        )

    def _extract_with_reference(
        self,
        freq: np.ndarray,
        samp_amp: np.ndarray,
        samp_phase: np.ndarray,
        ref_amp: np.ndarray,
        ref_phase: np.ndarray,
        d: float,
    ) -> Dict[str, List[float]]:
        omega = 2 * np.pi * freq

        valid_mask = (ref_amp > 1e-10) & (freq > 0)
        freq_valid = freq[valid_mask]
        omega_valid = omega[valid_mask]

        amp_ratio = np.ones_like(samp_amp)
        phase_diff = np.zeros_like(samp_phase)

        amp_ratio[valid_mask] = samp_amp[valid_mask] / ref_amp[valid_mask]
        phase_diff[valid_mask] = samp_phase[valid_mask] - ref_phase[valid_mask]

        phase_diff_valid = phase_diff[valid_mask]
        amp_ratio_valid = amp_ratio[valid_mask]

        n_valid = 1.0 - (self.c * phase_diff_valid) / (omega_valid * d)

        k_valid = np.zeros_like(n_valid)
        alpha_valid = np.zeros_like(n_valid)

        for i in range(len(amp_ratio_valid)):
            if amp_ratio_valid[i] > 0:
                n_i = n_valid[i]
                denom = (n_i + 1.0) ** 2

                if denom > 0:
                    transmission = amp_ratio_valid[i] * (4.0 * n_i) / denom
                    if transmission > 0:
                        k_valid[i] = -(self.c / (omega_valid[i] * d)) * np.log(transmission)
                        alpha_valid[i] = (4.0 * np.pi * freq_valid[i] * k_valid[i]) / self.c

        n = np.ones_like(freq)
        k = np.zeros_like(freq)
        alpha = np.zeros_like(freq)

        n[valid_mask] = n_valid
        k[valid_mask] = k_valid
        alpha[valid_mask] = alpha_valid

        alpha = self._smooth(alpha, window_len=5)
        n = self._smooth(n, window_len=5)

        return {
            "frequencies": freq.tolist(),
            "absorption_coeff": alpha.tolist(),
            "refractive_index": n.tolist(),
            "extinction_coeff": k.tolist(),
        }

    def _extract_without_reference(
        self,
        freq: np.ndarray,
        amp: np.ndarray,
        phase: np.ndarray,
        d: float,
    ) -> Dict[str, List[float]]:
        omega = 2 * np.pi * freq

        valid_mask = (amp > np.max(amp) * 0.01) & (freq > 0)

        n = np.ones_like(freq)
        k = np.zeros_like(freq)
        alpha = np.zeros_like(freq)

        if np.any(valid_mask):
            freq_valid = freq[valid_mask]
            omega_valid = omega[valid_mask]
            amp_valid = amp[valid_mask]

            log_amp = np.log(amp_valid)

            k[valid_mask] = (self.c / (omega_valid * d)) * (
                np.max(log_amp) - log_amp
            )

            alpha[valid_mask] = (4.0 * np.pi * freq_valid * k[valid_mask]) / self.c

            dn = 0.1 * np.exp(-((freq_valid - 0.5e12) / 1e12) ** 2)
            n[valid_mask] = 1.5 + dn

            n[valid_mask] = self._smooth(n[valid_mask], window_len=5)
            k[valid_mask] = self._smooth(k[valid_mask], window_len=5)
            alpha[valid_mask] = self._smooth(alpha[valid_mask], window_len=5)

        return {
            "frequencies": freq.tolist(),
            "absorption_coeff": alpha.tolist(),
            "refractive_index": n.tolist(),
            "extinction_coeff": k.tolist(),
        }

    def _smooth(self, x: np.ndarray, window_len: int = 5) -> np.ndarray:
        if len(x) < window_len:
            return x
        window = np.ones(window_len) / window_len
        return np.convolve(x, window, mode="same")

    def compute_complex_permittivity(
        self, n: List[float], k: List[float]
    ) -> Dict[str, List[float]]:
        n_arr = np.array(n)
        k_arr = np.array(k)

        epsilon_real = n_arr ** 2 - k_arr ** 2
        epsilon_imag = 2 * n_arr * k_arr

        return {
            "epsilon_real": epsilon_real.tolist(),
            "epsilon_imag": epsilon_imag.tolist(),
        }

    def compute_power_absorption_coefficient(
        self, alpha: List[float]
    ) -> List[float]:
        return [a / 4.343 for a in alpha]
