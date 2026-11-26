package com.trackferry.tv

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.log10
import kotlin.math.sin
import kotlin.math.sqrt

class SoundTest {
    private fun sine(freq: Double, rate: Int, frames: Int, channels: Int = 2, level: Float = .25f) =
        FloatArray(frames * channels) { level * sin(2 * PI * freq * (it / channels) / rate).toFloat() }

    private fun rms(x: FloatArray, from: Int = x.size / 2) = sqrt(x.drop(from).map { it * it.toDouble() }.average())

    private fun gainDb(preset: String, freq: Double, rate: Int = 48_000): Double {
        val eq = Equalizer().apply { set(eqPreset(preset), rate) }
        val input = sine(freq, rate, rate / 2)
        val output = input.copyOf()
        eq.process(output, rate / 2, 2)
        return 20 * log10(rms(output) / rms(input))
    }

    @Test fun presetsMatchTheWeb() {
        assertEquals(listOf("flat", "pop", "rock", "vocal", "classical", "electronic", "bass", "stereo"), EQ_PRESETS.map { it.name })
        assertEquals("flat", eqPreset("nonsense").name)
        assertEquals(-6f, eqPreamp(eqPreset("bass")), 0f)
        assertEquals(-3f, eqPreamp(eqPreset("stereo")), 0f) // 1.5 dB boost and 1.5 dB for the width
        assertEquals(0f, eqPreamp(eqPreset("flat")), 0f)
    }

    @Test fun flatLeavesTheSoundAlone() {
        val eq = Equalizer().apply { set(eqPreset("flat"), 44_100) }
        val input = sine(440.0, 44_100, 2048)
        val output = input.copyOf()
        eq.process(output, 2048, 2)
        assertTrue(input.contentEquals(output))
    }

    @Test fun bassLiftsTheLowsAgainstTheMids() {
        // 低音: +6 dB shelf at 60 Hz, all of it taken back by a 6 dB preamp.
        val low = gainDb("bass", 40.0)
        val mid = gainDb("bass", 2_000.0)
        assertEquals(-6.0, mid, .6)
        assertTrue("lows $low mids $mid", low - mid > 4.5)
        assertTrue("never louder than the input: $low", low < .5)
    }

    @Test fun vocalLiftsThePresence() {
        assertTrue(gainDb("vocal", 3_600.0) - gainDb("vocal", 60.0) > 4)
    }

    @Test fun peakingBandHitsItsGainAtTheCentre() {
        val q = Biquad().apply { set(1, 1_000f, 6f, 48_000) }
        val input = sine(1_000.0, 48_000, 24_000, channels = 1)
        val out = FloatArray(input.size) { q.run(input[it].toDouble(), 0).toFloat() }
        assertEquals(6.0, 20 * log10(rms(out) / rms(input)), .3)
    }

    @Test fun stereoWidensOnlyTheSides() {
        val eq = Equalizer().apply { set(eqPreset("stereo"), 48_000) }
        // The same sound on both sides (mid only) is not widened.
        val mono = FloatArray(2 * 4800) { .2f * sin(2 * PI * 1000 * (it / 2) / 48_000).toFloat() }
        eq.process(mono, 4800, 2)
        for (f in 2400 until 4800) assertEquals(mono[f * 2], mono[f * 2 + 1], 1e-6f)
    }

    @Test fun outputStaysInRange() {
        val eq = Equalizer().apply { set(eqPreset("rock"), 44_100) }
        val loud = sine(60.0, 44_100, 8192, level = 1f)
        eq.process(loud, 8192, 2)
        assertTrue(loud.all { it in -1f..1f })
    }

    @Test fun analyzerFindsTheTone() {
        val rate = 44_100
        val analyzer = Analyzer(1024)
        val levels = FloatArray(Spectrum.BANDS)
        analyzer.levels(FloatArray(1024) { sin(2 * PI * 1_000 * it / rate).toFloat() }, rate, levels)
        val loudest = levels.indices.maxBy { levels[it] }
        // 1 kHz sits in the band whose range holds it.
        val low = kotlin.math.ln(40.0); val high = kotlin.math.ln(16_000.0)
        val expected = ((kotlin.math.ln(1_000.0) - low) / (high - low) * Spectrum.BANDS).toInt()
        assertTrue("band $loudest, expected about $expected", abs(loudest - expected) <= 1)
        assertTrue(levels[loudest] > .8f)
        analyzer.levels(FloatArray(1024), rate, levels)
        assertTrue(levels.all { it == 0f })
    }

    @Test fun spectrumShowsTheFrameBeingHeard() {
        Spectrum.clear()
        Spectrum.playoutUs = Long.MIN_VALUE
        val out = FloatArray(Spectrum.BANDS)
        assertFalse(Spectrum.now(out))
        Spectrum.add(1_000_000, FloatArray(Spectrum.BANDS) { .1f })
        Spectrum.add(1_020_000, FloatArray(Spectrum.BANDS) { .2f })
        Spectrum.add(1_040_000, FloatArray(Spectrum.BANDS) { .3f })
        Spectrum.playoutUs = 1_025_000
        assertTrue(Spectrum.now(out))
        assertEquals(.2f, out[0], 0f)
        Spectrum.playoutUs = 2_000_000 // far past what was measured: nothing to show
        assertFalse(Spectrum.now(out))
        Spectrum.clear()
    }

    @Test fun barsFoldBandsByTheirPeak() {
        val frame = FloatArray(24) { it / 24f }
        assertEquals(5 / 24f, bandOf(frame, 0, 4), 1e-6f)
        assertEquals(23 / 24f, bandOf(frame, 3, 4), 1e-6f)
        assertEquals(frame[7], bandOf(frame, 7, 24), 0f)
    }

    @Test fun updateComparesVersions() {
        assertTrue(TvUpdate.newer("1.7.0", "1.6.0"))
        assertTrue(TvUpdate.newer("1.6.10", "1.6.9"))
        assertTrue(TvUpdate.newer("v2.0.0", "1.99.99"))
        assertFalse(TvUpdate.newer("1.6.0", "1.6.0"))
        assertFalse(TvUpdate.newer("1.5.9", "1.6.0"))
        assertFalse(TvUpdate.newer("dev", "1.6.0"))
        assertTrue(TvUpdate.newer("1.6.0", ""))
    }
}
