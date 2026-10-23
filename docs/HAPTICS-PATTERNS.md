# Feedback patterns

Neutral names, technical definitions. The engine runs at 30 ms ticks in `hapticsd`; each pattern returns a low/high amplitude pair in 0..1, scaled by intensity.

| Name | Model | Notes |
|---|---|---|
| **Audio** | real-time band energy | low <120 Hz, high 2–8 kHz, 60 ms update; attack 20 ms / release 120 ms. |
| **Steady** | constant 0.65 / 0.35 | reference level; useful for testing latency. |
| **Firm** | constant 0.95 / 0.60 | maximum practical duty for DS4 ERM motors; short sessions only. |
| **Wave** | 0.25 Hz sine, ±0.35 low | slow rocking; keeps both motors moving without saturation. |
| **Build** | 18 s ramp → 6 s hold, loop | linear ramp; matches the "excitement" phase. |
| **Hold** | 12 s ramp → 8 s plateau → 1.5 s withdrawal → 3 s rest, loop | Masters & Johnson's plateau/withdrawal shape; the 1.5 s fall is the key cue. |
| **Drop** | 10 s ramp → abrupt 0 for 1.2 s → 0.15 rest, loop | sharper withdrawal than Hold. |
| **Beat** | 1.25 Hz, 160 ms duty | pulse width long enough for ERM to spin up; gap > 50 ms so pulses stay distinct. |
| **Surge** | 1.2 s high burst every 5 s | contrasts with a low background; good for syncopated audio. |
| **Drift** | two slow LFOs (0.33/0.77 Hz) | non-repeating feel; avoids habituation. |

## Design notes

- **ERM motors** need ~50–80 ms to reach speed; pulses shorter than that are felt as clicks. Android's haptics guidance recommends ≥50 ms gaps between primitives; the Beat pattern uses 160 ms on / 640 ms off.
- **Amplitude is not linear in perceived intensity**; the daemon smooths targets (attack 0.35, release 0.15 per tick) so ramps do not step.
- **Two motors**: low band drives the large/left motor, high band the small/right motor. Keep low ≥ high for body-weight patterns; invert for detail.
- **Latency**: USB DualShock 4 ≈ 5–15 ms; Bluetooth 30–100 ms. Patterns are generated on the server, so client latency does not shift the envelope.
- **Plateau / withdrawal** is a documented arousal-control shape (Masters & Johnson, 1966; see general summaries of the sexual response cycle). The daemon only generates vibration envelopes; it has no physiological sensing.

## Sources

- Android Developers — *Add haptic feedback to events* / *Create custom haptic effects* (50 ms gap guidance, waveform primitives).
- Microsoft — *Input Device Haptics Implementation Guide* (waveform and duty-cycle guidance).
- Springer — *Perceptual and Technological Issues in the Design of Vibrotactile-Augmented Interfaces for Music Technology and Media* (amplitude/frequency perception).
- Springer — *Vibrotactile Similarity Perception in Crowdsourced and Lab Studies* (pattern similarity).
- Masters & Johnson (1966), sexual response cycle — plateau and resolution phases (general summaries).
