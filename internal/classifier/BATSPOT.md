# BatSpot m09

BatSpot m09 classifies search-phase calls from European bats. The checkpoint is
by Smeele et al., _Biological Conservation_ 321 (2026), article 111975. The
weights are licensed CC BY 4.0. The ONNX file adapts the published PyTorch
checkpoint for ONNX Runtime. [Paper](https://doi.org/10.1016/j.biocon.2026.111975),
[weights](https://doi.org/10.5281/zenodo.18977979),
[ANIMAL-SPOT code](https://github.com/Hauechri/BatSpot),
[license](https://creativecommons.org/licenses/by/4.0/).

Install BatSpot in the AI Models gallery. The ONNX conversion by alexbelgium
and its labels are hosted on [Hugging Face](https://huggingface.co/alexbelgium/BatSpot).
Enable `batspot` and select it for an ultrasonic source capturing at 256 or 384 kHz
(resampled to the model's 250 kHz). BatSpot shares the nighttime schedule in the `bat` settings.

For a manual install, set the model and [label](testdata/batspot/batspot_labels.txt)
paths in `config.yaml` or `/api/v2/settings/batspot` (`modelPath`, `labelPath`
and `threshold`).

```yaml
batspot:
  modelpath: /models/batspot_m09.onnx
  labelpath: /models/batspot_labels.txt
  threshold: 0.5
```

BatSpot uses a 1.010-second clip with a one-second step. The 10 ms overlap
preserves the 20 ms candidates that straddle a step boundary. The gate measures
STFT band power at a 10 ms hop over the model input's FFT bins (10–127, about
10–125 kHz, no Nyquist), skips digital silence and retains windows at least
6 dB above the clip median and above an absolute floor of -85 dBFS (so dither
in a mostly-zero clip cannot pass). It takes up to four ranked,
non-overlapping windows in one ONNX batch. A clip with no eligible calls skips
ONNX entirely, except the first one: the orchestrator warms up with a silent
clip, so BatSpot scores one zero row the first time to pay session allocation
and first-run latency at warm-up. Feeding buzz, noise and social calls contribute to softmax
normalization, and results report the maximum probability for each species.

There are at most 100 inexpensive STFT checks and four ONNX rows per second of
audio. At the export benchmark of about 63 ms per window on one Intel N150
thread, the four-row budget is about 250 ms of inference per second of audio,
plus the gate and preprocessing. The startup latency probe runs after warm-up and
measures the silence path.

The parity fixtures include a resampled window from the repository's weak bat
call recording, a synthetic frequency sweep, and silence. Their inputs and
expected scores were exported from the upstream PyTorch preprocessing and
checkpoint. The functional test accepts `BATSPOT_TEST_MODEL`,
`BATSPOT_TEST_LABELS` and `ONNX_TEST_ORT_LIB` and runs under the `onnx` build tag.
