package com.trackferry.tv

import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.ln
import kotlin.math.log10
import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow
import kotlin.math.sin
import kotlin.math.sqrt

/**
 * Sound presets, the same as web/src/eq.ts: five bands (a low shelf, three peaks, a high shelf)
 * and, for 立体声, a wider stage. Change both together.
 */
internal data class EqPreset(val name: String, val label: String, val gains: FloatArray, val width: Float)

internal val EQ_BANDS = floatArrayOf(60f, 230f, 910f, 3600f, 14000f)

internal val EQ_PRESETS = listOf(
    EqPreset("flat", "原声", floatArrayOf(0f, 0f, 0f, 0f, 0f), 1f),
    EqPreset("pop", "流行", floatArrayOf(-1f, 2.5f, 3.5f, 1.5f, -.5f), 1f),
    EqPreset("rock", "摇滚", floatArrayOf(4f, 2f, -2f, 2.5f, 4f), 1f),
    EqPreset("vocal", "人声", floatArrayOf(-2f, -1f, 3f, 3.5f, 1f), 1f),
    EqPreset("classical", "古典", floatArrayOf(3f, 1f, 0f, 1f, 3f), 1f),
    EqPreset("electronic", "电子", floatArrayOf(4.5f, 1.5f, 0f, 2f, 4f), 1f),
    EqPreset("bass", "低音", floatArrayOf(6f, 3.5f, 0f, 0f, 0f), 1f),
    EqPreset("stereo", "立体声", floatArrayOf(1f, 0f, 0f, 0f, 1.5f), 1.6f),
)

internal fun eqPreset(name: String) = EQ_PRESETS.firstOrNull { it.name == name } ?: EQ_PRESETS[0]

/** How far everything is turned down first, in dB, so boosted bands do not clip. */
internal fun eqPreamp(p: EqPreset) = -max(0f, p.gains.max()) - if (p.width > 1f) 1.5f else 0f

/**
 * One biquad, made as the Web Audio API makes its own (Audio EQ Cookbook; shelves with S = 1,
 * peaks with Q = 1), so a preset sounds alike in the browser and on the television.
 */
internal class Biquad {
    private var b0 = 1.0; private var b1 = 0.0; private var b2 = 0.0; private var a1 = 0.0; private var a2 = 0.0
    private val x1 = DoubleArray(2); private val x2 = DoubleArray(2); private val y1 = DoubleArray(2); private val y2 = DoubleArray(2)

    fun set(kind: Int, frequency: Float, gainDb: Float, rate: Int) {
        val f = min(frequency.toDouble(), rate * .45)
        val a = 10.0.pow(gainDb / 40.0)
        val w = 2 * PI * f / rate
        val cw = cos(w)
        val sw = sin(w)
        var nb0: Double; var nb1: Double; var nb2: Double; var na0: Double; var na1: Double; var na2: Double
        when (kind) {
            0, 2 -> { // low shelf, high shelf, S = 1
                val alpha = sw / 2 * sqrt(2.0)
                val k = 2 * sqrt(a) * alpha
                val s = if (kind == 0) 1.0 else -1.0
                nb0 = a * ((a + 1) - s * (a - 1) * cw + k)
                nb1 = s * 2 * a * ((a - 1) - s * (a + 1) * cw)
                nb2 = a * ((a + 1) - s * (a - 1) * cw - k)
                na0 = (a + 1) + s * (a - 1) * cw + k
                na1 = -s * 2 * ((a - 1) + s * (a + 1) * cw)
                na2 = (a + 1) + s * (a - 1) * cw - k
            }
            else -> { // peak, Q = 1
                val alpha = sw / 2
                nb0 = 1 + alpha * a; nb1 = -2 * cw; nb2 = 1 - alpha * a
                na0 = 1 + alpha / a; na1 = -2 * cw; na2 = 1 - alpha / a
            }
        }
        b0 = nb0 / na0; b1 = nb1 / na0; b2 = nb2 / na0; a1 = na1 / na0; a2 = na2 / na0
    }

    fun reset() { x1.fill(0.0); x2.fill(0.0); y1.fill(0.0); y2.fill(0.0) }

    fun run(x: Double, ch: Int): Double {
        val y = b0 * x + b1 * x1[ch] + b2 * x2[ch] - a1 * y1[ch] - a2 * y2[ch]
        x2[ch] = x1[ch]; x1[ch] = x
        y2[ch] = y1[ch]; y1[ch] = if (kotlin.math.abs(y) < 1e-25) 0.0 else y
        return y
    }
}

/** The preset chain for interleaved float samples in place: preamp, five bands, then the width. */
internal class Equalizer {
    private val bands = Array(5) { Biquad() }
    private var gain = 1.0
    private var a = 1.0
    private var b = 0.0
    var preset: EqPreset = EQ_PRESETS[0]
        private set
    val flat get() = preset.name == "flat"

    fun set(p: EqPreset, rate: Int) {
        if (p.name != preset.name) for (band in bands) band.reset()
        preset = p
        for (i in bands.indices) bands[i].set(if (i == 0) 0 else if (i == bands.lastIndex) 2 else 1, EQ_BANDS[i], p.gains[i], rate)
        gain = 10.0.pow(eqPreamp(p) / 20.0)
        a = (1.0 + p.width) / 2
        b = (1.0 - p.width) / 2
    }

    fun reset() { for (band in bands) band.reset() }

    /** frames × channels samples; channels 1 or 2. */
    fun process(samples: FloatArray, frames: Int, channels: Int) {
        if (flat) return
        for (f in 0 until frames) {
            val at = f * channels
            for (c in 0 until channels) {
                var x = samples[at + c] * gain
                for (band in bands) x = band.run(x, c)
                samples[at + c] = x.toFloat()
            }
            if (channels == 2 && b != 0.0) {
                val l = samples[at].toDouble()
                val r = samples[at + 1].toDouble()
                samples[at] = (a * l + b * r).toFloat()
                samples[at + 1] = (a * r + b * l).toFloat()
            }
            for (c in 0 until channels) samples[at + c] = samples[at + c].coerceIn(-1f, 1f)
        }
    }
}

/**
 * The spectrum of what is playing, measured from the decoded sound and kept by its presentation
 * time; the screen shows the frame that matches what the speakers play now, not an imitation.
 */
internal class Analyzer(private val size: Int = 1024) {
    private val window = FloatArray(size) { (.5 - .5 * cos(2 * PI * it / (size - 1))).toFloat() }
    private val re = FloatArray(size)
    private val im = FloatArray(size)
    private val bits = Integer.numberOfTrailingZeros(size)

    init { require(size >= 64 && size and (size - 1) == 0) }

    /** Levels 0..1 of BANDS bands spaced evenly in pitch between 40 Hz and 16 kHz. */
    fun levels(mono: FloatArray, rate: Int, out: FloatArray) {
        for (i in 0 until size) { re[i] = mono[i] * window[i]; im[i] = 0f }
        fft()
        val bands = out.size
        val low = ln(40.0)
        val high = ln(min(16000.0, rate / 2.0 - 1))
        for (band in 0 until bands) {
            val f0 = kotlin.math.exp(low + (high - low) * band / bands)
            val f1 = kotlin.math.exp(low + (high - low) * (band + 1) / bands)
            val i0 = max(1, (f0 * size / rate).toInt())
            val i1 = max(i0 + 1, (f1 * size / rate).toInt())
            var peak = 0f
            for (i in i0 until min(i1, size / 2)) peak = max(peak, re[i] * re[i] + im[i] * im[i])
            // A full-scale sine reads about (size/4)²; 0 dB at the top, -60 dB at the floor.
            val db = 10 * log10(peak / ((size / 4f) * (size / 4f)) + 1e-12)
            out[band] = ((db + 60) / 60).toFloat().coerceIn(0f, 1f)
        }
    }

    private fun fft() {
        for (i in 0 until size) {
            val j = Integer.reverse(i) ushr (32 - bits)
            if (j > i) {
                val tr = re[i]; re[i] = re[j]; re[j] = tr
                val ti = im[i]; im[i] = im[j]; im[j] = ti
            }
        }
        var len = 2
        while (len <= size) {
            val step = -2 * PI / len
            for (start in 0 until size step len) {
                for (k in 0 until len / 2) {
                    val c = cos(step * k).toFloat()
                    val s = sin(step * k).toFloat()
                    val a = start + k
                    val b = a + len / 2
                    val tr = re[b] * c - im[b] * s
                    val ti = re[b] * s + im[b] * c
                    re[b] = re[a] - tr; im[b] = im[a] - ti
                    re[a] += tr; im[a] += ti
                }
            }
            len = len shl 1
        }
    }
}

/** Frames of the spectrum by presentation time, written by the player's thread, read by the screen. */
internal object Spectrum {
    const val BANDS = 24
    private const val KEEP = 256
    private val lock = Any()
    private val times = LongArray(KEEP)
    private val frames = Array(KEEP) { FloatArray(BANDS) }
    private var next = 0
    private var count = 0

    /** Where the speakers are now, in the same clock as the frames' times. */
    @Volatile var playoutUs = Long.MIN_VALUE

    fun clear() = synchronized(lock) { count = 0; next = 0 }

    fun add(at: Long, levels: FloatArray) = synchronized(lock) {
        times[next] = at
        levels.copyInto(frames[next])
        next = (next + 1) % KEEP
        if (count < KEEP) count++
    }

    /** Copies the frame playing now into out; false when there is none close enough. */
    fun now(out: FloatArray): Boolean {
        val t = playoutUs
        synchronized(lock) {
            var best = -1
            var bestAt = Long.MIN_VALUE
            for (i in 0 until count) {
                val at = times[i]
                if (at <= t && at > bestAt) { bestAt = at; best = i }
            }
            if (best < 0 || t - bestAt > 250_000) return false
            frames[best].copyInto(out)
            return true
        }
    }
}
