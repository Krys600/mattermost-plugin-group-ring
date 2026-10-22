// A synthesized ringtone (two short dual-tone bursts every three seconds), so the plugin ships no
// audio files. Browsers may refuse to play audio before the user interacted with the page; the
// incoming-call modal is still shown in that case.

type AudioContextConstructor = typeof AudioContext;

const PATTERN_INTERVAL_MS = 3000;
const FREQUENCIES = [440, 480];
const BURSTS = [{start: 0, duration: 0.4}, {start: 0.6, duration: 0.4}];
const VOLUME = 0.12;

export class Ringtone {
    private context: AudioContext | null = null;
    private timer: ReturnType<typeof setInterval> | null = null;

    get playing(): boolean {
        return this.timer !== null;
    }

    start() {
        if (this.timer !== null) {
            return;
        }
        const Ctor: AudioContextConstructor | undefined = window.AudioContext || (window as unknown as {webkitAudioContext?: AudioContextConstructor}).webkitAudioContext;
        if (Ctor) {
            try {
                this.context = new Ctor();
                this.context.resume().catch(() => undefined);
            } catch {
                this.context = null;
            }
        }
        this.playPattern();
        this.timer = setInterval(() => this.playPattern(), PATTERN_INTERVAL_MS);
    }

    stop() {
        if (this.timer !== null) {
            clearInterval(this.timer);
            this.timer = null;
        }
        if (this.context) {
            this.context.close().catch(() => undefined);
            this.context = null;
        }
    }

    private playPattern() {
        const ctx = this.context;
        if (!ctx || ctx.state === 'closed') {
            return;
        }
        const now = ctx.currentTime;
        for (const burst of BURSTS) {
            const gain = ctx.createGain();
            gain.gain.setValueAtTime(0, now + burst.start);
            gain.gain.linearRampToValueAtTime(VOLUME, now + burst.start + 0.02);
            gain.gain.setValueAtTime(VOLUME, now + burst.start + (burst.duration - 0.02));
            gain.gain.linearRampToValueAtTime(0, now + burst.start + burst.duration);
            gain.connect(ctx.destination);
            for (const frequency of FREQUENCIES) {
                const osc = ctx.createOscillator();
                osc.frequency.value = frequency;
                osc.connect(gain);
                osc.start(now + burst.start);
                osc.stop(now + burst.start + burst.duration);
            }
        }
    }
}
