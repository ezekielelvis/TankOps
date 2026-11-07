#!/usr/bin/env python3
"""Synthesises the game's sound effects into one WAV file.

Run from the repository root:

    python3 tools/gen_assets/gen_sounds.py

Output: web/dist/assets/sounds.wav

The clips sit at fixed offsets that web/src/audio-driver.ts reads (keep the
two in sync):

     1.0 - 15.0 s  engine rumble (loops)
    19.5 - 23.0 s  turret servo (loops)
    24.88 (+1.1)   cannon shot
    29.0  (+4.0)   explosion
    34.1 - 37.2 s  wind ambience (loops)

Looping clips are built from whole periods of every component so the loop
point is seamless. Requires numpy.
"""
import wave
from pathlib import Path

import numpy as np

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / "web/dist/assets/sounds.wav"
SR = 22050
rng = np.random.default_rng(20240607)


def t_axis(seconds):
    return np.arange(int(seconds * SR)) / SR


def periodic_noise(seconds, lo, hi, tilt=0.0):
    """Band-limited noise that repeats exactly every `seconds`."""
    n = int(seconds * SR)
    spec = np.zeros(n // 2 + 1, dtype=complex)
    freqs = np.fft.rfftfreq(n, 1 / SR)
    band = (freqs >= lo) & (freqs <= hi)
    phase = rng.uniform(0, 2 * np.pi, band.sum())
    amp = np.ones(band.sum())
    if tilt:
        amp = (np.maximum(freqs[band], 1.0)) ** tilt
    spec[band] = amp * np.exp(1j * phase)
    x = np.fft.irfft(spec, n)
    return x / (np.abs(x).max() + 1e-9)


def lowpass(x, cutoff):
    """One-pole low-pass (fine for noise shaping)."""
    a = np.exp(-2 * np.pi * cutoff / SR)
    y = np.zeros_like(x)
    acc = 0.0
    for i, v in enumerate(x):
        acc = (1 - a) * v + a * acc
        y[i] = acc
    return y


def engine(seconds):
    t = t_axis(seconds)
    base = 1.0 / seconds  # all frequencies are whole multiples of this
    f0 = round(34 / base) * base
    sig = np.zeros_like(t)
    for k, amp in enumerate([1.0, 0.7, 0.5, 0.32, 0.2, 0.12], start=1):
        sig += amp * np.sin(2 * np.pi * f0 * k * t + k)
    wobble = 1 + 0.25 * np.sin(2 * np.pi * (2 / seconds) * t) + 0.12 * np.sin(2 * np.pi * (7 / seconds) * t)
    rumble = periodic_noise(seconds, 25, 260, tilt=-0.4)
    clatter = periodic_noise(seconds, 400, 1500) * (0.5 + 0.5 * np.sin(2 * np.pi * (28 / seconds) * t)) ** 2
    out = sig / 2.4 * wobble + 0.55 * rumble + 0.12 * clatter
    return 0.8 * out / np.abs(out).max()


def servo(seconds):
    t = t_axis(seconds)
    base = 1.0 / seconds
    f = round(210 / base) * base
    wob = 1 + 0.05 * np.sin(2 * np.pi * (3 / seconds) * t)
    tone = np.sin(2 * np.pi * f * t * wob) + 0.4 * np.sin(2 * np.pi * 2 * f * t) + 0.2 * np.sin(2 * np.pi * 3 * f * t)
    hiss = periodic_noise(seconds, 800, 3500)
    out = 0.6 * tone / 1.6 + 0.35 * hiss
    return 0.5 * out / np.abs(out).max()


def cannon(seconds):
    t = t_axis(seconds)
    crack = rng.uniform(-1, 1, t.size) * np.exp(-t * 55)
    crack = crack - lowpass(crack, 900)  # bright part
    boom = rng.uniform(-1, 1, t.size)
    boom = lowpass(lowpass(boom, 380), 380) * 6 * np.exp(-t * 7)
    freq = 110 * np.exp(-t * 4.5) + 28
    thump = np.sin(2 * np.pi * np.cumsum(freq) / SR) * np.exp(-t * 6)
    tail = lowpass(rng.uniform(-1, 1, t.size), 1500) * np.exp(-t * 3) * (t > 0.08) * 0.6
    out = 0.9 * crack + 0.9 * boom + 1.1 * thump + tail
    out *= np.minimum(1, t * 800)
    return 0.95 * out / np.abs(out).max()


def explosion(seconds):
    t = t_axis(seconds)
    n = rng.uniform(-1, 1, t.size)
    rumble = lowpass(lowpass(n, 260), 260) * 9 * np.exp(-t * 1.1)
    mid = lowpass(n, 1200) * np.exp(-t * 2.6)
    freq = 90 * np.exp(-t * 1.6) + 22
    thump = np.sin(2 * np.pi * np.cumsum(freq) / SR) * np.exp(-t * 1.7)
    crackle = np.zeros_like(t)
    for _ in range(46):
        at = rng.uniform(0.05, 2.6)
        i = int(at * SR)
        length = int(rng.uniform(0.004, 0.02) * SR)
        if i + length < t.size:
            crackle[i:i + length] += rng.uniform(-1, 1, length) * np.exp(-np.arange(length) / (length / 4)) * rng.uniform(0.2, 0.8) * np.exp(-at * 0.7)
    out = 1.2 * rumble + 0.7 * mid + 1.3 * thump + 0.35 * crackle
    out *= np.minimum(1, t * 600)
    out *= np.minimum(1, (seconds - t) * 4)  # fade the tail to silence
    return 0.95 * out / np.abs(out).max()


def wind(seconds):
    t = t_axis(seconds)
    n = periodic_noise(seconds, 120, 1600, tilt=-0.7)
    gust = 0.65 + 0.35 * np.sin(2 * np.pi * (2 / seconds) * t + 1.0)
    return 0.7 * n * gust


def place(buf, start, clip):
    i = int(round(start * SR))
    buf[i:i + clip.size] += clip


def main():
    total = 38.0
    buf = np.zeros(int(total * SR))
    place(buf, 1.0, engine(14.0))
    place(buf, 19.5, servo(3.5))
    place(buf, 24.88, cannon(1.1))
    place(buf, 29.0, explosion(4.0))
    place(buf, 34.1, wind(3.1))
    buf = np.clip(buf, -1, 1)
    pcm = (buf * 32767).astype("<i2")
    OUT.parent.mkdir(parents=True, exist_ok=True)
    with wave.open(str(OUT), "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(SR)
        w.writeframes(pcm.tobytes())
    print(f"wrote {OUT.relative_to(ROOT)} ({OUT.stat().st_size // 1024} KiB)")


if __name__ == "__main__":
    main()
