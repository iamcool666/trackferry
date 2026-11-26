package com.trackferry.tv

import android.content.Context
import androidx.media3.common.C
import androidx.media3.common.Format
import androidx.media3.common.MimeTypes
import androidx.media3.common.audio.AudioProcessor
import androidx.media3.common.audio.BaseAudioProcessor
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.audio.AudioSink
import androidx.media3.exoplayer.audio.DefaultAudioSink
import androidx.media3.exoplayer.audio.ForwardingAudioSink
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import java.nio.ByteBuffer
import java.nio.ByteOrder

/** The preset this television plays with, kept on the television. */
internal object SoundSettings {
    private val chosen = MutableStateFlow("flat")
    val preset: StateFlow<String> = chosen
    @Volatile var current = "flat"
        private set

    fun load(context: Context) {
        val name = eqPreset(context.getSharedPreferences("sound", Context.MODE_PRIVATE).getString("eq", "flat").orEmpty()).name
        current = name
        chosen.value = name
    }

    fun choose(context: Context, name: String) {
        val p = eqPreset(name).name
        current = p
        chosen.value = p
        context.getSharedPreferences("sound", Context.MODE_PRIVATE).edit().putString("eq", p).apply()
    }
}

/** Plays through the preset chain and measures what it plays. */
@UnstableApi
internal class SoundRenderers(context: Context) : DefaultRenderersFactory(context) {
    override fun buildAudioSink(context: Context, enableFloatOutput: Boolean, enableAudioTrackPlaybackParams: Boolean): AudioSink =
        TapSink(DefaultAudioSink.Builder(context)
            .setEnableFloatOutput(enableFloatOutput)
            .setEnableAudioTrackPlaybackParams(enableAudioTrackPlaybackParams)
            .setAudioProcessors(arrayOf<AudioProcessor>(EqProcessor()))
            .build())
}

/**
 * Applies the preset to 16-bit and float sound of one or two channels. Anything else passes as it
 * is, and a fault in the arithmetic lets the sound through untouched rather than stopping it.
 */
@UnstableApi
internal class EqProcessor : BaseAudioProcessor() {
    private val eq = Equalizer()
    private var rate = 0
    private var samples = FloatArray(0)

    override fun onConfigure(inputAudioFormat: AudioProcessor.AudioFormat): AudioProcessor.AudioFormat {
        val ok = (inputAudioFormat.encoding == C.ENCODING_PCM_16BIT || inputAudioFormat.encoding == C.ENCODING_PCM_FLOAT) &&
            inputAudioFormat.channelCount in 1..2
        return if (ok) inputAudioFormat else AudioProcessor.AudioFormat.NOT_SET
    }

    override fun queueInput(inputBuffer: ByteBuffer) {
        val size = inputBuffer.remaining()
        if (size == 0) return
        val format = inputAudioFormat
        val want = SoundSettings.current
        if (want != eq.preset.name || rate != format.sampleRate) {
            eq.set(eqPreset(want), format.sampleRate)
            rate = format.sampleRate
        }
        val out = replaceOutputBuffer(size)
        if (eq.flat) { out.put(inputBuffer); out.flip(); return }
        val start = inputBuffer.position()
        try {
            val float = format.encoding == C.ENCODING_PCM_FLOAT
            val count = size / if (float) 4 else 2
            if (samples.size < count) samples = FloatArray(count)
            val input = inputBuffer.duplicate().order(ByteOrder.nativeOrder())
            if (float) for (i in 0 until count) samples[i] = input.getFloat()
            else for (i in 0 until count) samples[i] = input.getShort() / 32768f
            eq.process(samples, count / format.channelCount, format.channelCount)
            if (float) for (i in 0 until count) out.putFloat(samples[i])
            else for (i in 0 until count) out.putShort(Math.round(samples[i] * 32767f).coerceIn(-32768, 32767).toShort())
            inputBuffer.position(inputBuffer.limit())
        } catch (_: RuntimeException) {
            out.clear()
            inputBuffer.position(start)
            out.put(inputBuffer)
            eq.reset()
        }
        out.flip()
    }

    override fun onFlush() { eq.reset() }
    override fun onReset() { rate = 0; samples = FloatArray(0) }
}

/**
 * Measures the decoded sound by its presentation time and reports where the speakers are, so the
 * spectrum on screen is the one being heard. It never changes the sound.
 */
@UnstableApi
internal class TapSink(sink: AudioSink) : ForwardingAudioSink(sink) {
    private val analyzer = Analyzer(1024)
    private val mono = FloatArray(1024)
    private val levels = FloatArray(Spectrum.BANDS)
    private var filled = 0
    private var windowUs = 0L
    private var bytes = 0
    private var encoding = C.ENCODING_INVALID
    private var channels = 0
    private var rate = 0
    private var lastPts = Long.MIN_VALUE
    private var expectUs = Long.MIN_VALUE

    override fun configure(inputFormat: Format, specifiedBufferSize: Int, outputChannels: IntArray?) {
        encoding = if (inputFormat.sampleMimeType == MimeTypes.AUDIO_RAW) inputFormat.pcmEncoding else C.ENCODING_INVALID
        bytes = when (encoding) {
            C.ENCODING_PCM_16BIT -> 2
            C.ENCODING_PCM_24BIT -> 3
            C.ENCODING_PCM_32BIT, C.ENCODING_PCM_FLOAT -> 4
            else -> 0
        }
        channels = inputFormat.channelCount
        rate = inputFormat.sampleRate
        filled = 0
        super.configure(inputFormat, specifiedBufferSize, outputChannels)
    }

    override fun handleBuffer(buffer: ByteBuffer, presentationTimeUs: Long, encodedAccessUnitCount: Int): Boolean {
        if (presentationTimeUs != lastPts) {
            lastPts = presentationTimeUs
            try { look(buffer, presentationTimeUs) } catch (_: RuntimeException) { filled = 0 }
        }
        return super.handleBuffer(buffer, presentationTimeUs, encodedAccessUnitCount)
    }

    private fun look(buffer: ByteBuffer, pts: Long) {
        if (bytes == 0 || channels <= 0 || rate <= 0) return
        val frame = bytes * channels
        val frames = buffer.remaining() / frame
        if (frames == 0) return
        if (expectUs != Long.MIN_VALUE && kotlin.math.abs(pts - expectUs) > 50_000) filled = 0 // a jump: start a new window
        expectUs = pts + frames * 1_000_000L / rate
        val data = buffer.duplicate().order(ByteOrder.nativeOrder())
        val base = buffer.position()
        for (f in 0 until frames) {
            if (filled == 0) windowUs = pts + f * 1_000_000L / rate
            var sum = 0f
            for (c in 0 until channels) sum += sample(data, base + f * frame + c * bytes)
            mono[filled++] = sum / channels
            if (filled == mono.size) {
                analyzer.levels(mono, rate, levels)
                Spectrum.add(windowUs + mono.size * 500_000L / rate, levels)
                filled = 0
            }
        }
    }

    private fun sample(data: ByteBuffer, at: Int): Float = when (encoding) {
        C.ENCODING_PCM_16BIT -> data.getShort(at) / 32768f
        C.ENCODING_PCM_FLOAT -> data.getFloat(at)
        C.ENCODING_PCM_32BIT -> data.getInt(at) / 2147483648f
        else -> { // 24-bit, little-endian
            val v = (data.get(at).toInt() and 255) or ((data.get(at + 1).toInt() and 255) shl 8) or (data.get(at + 2).toInt() shl 16)
            v / 8388608f
        }
    }

    override fun getCurrentPositionUs(sourceEnded: Boolean): Long {
        val at = super.getCurrentPositionUs(sourceEnded)
        if (at != AudioSink.CURRENT_POSITION_NOT_SET) Spectrum.playoutUs = at
        return at
    }

    override fun flush() {
        filled = 0; lastPts = Long.MIN_VALUE; expectUs = Long.MIN_VALUE
        Spectrum.clear()
        super.flush()
    }

    override fun reset() {
        filled = 0; lastPts = Long.MIN_VALUE; expectUs = Long.MIN_VALUE
        Spectrum.clear()
        super.reset()
    }
}
