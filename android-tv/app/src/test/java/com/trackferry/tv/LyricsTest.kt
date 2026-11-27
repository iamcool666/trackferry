package com.trackferry.tv

import org.junit.Assert.assertEquals
import org.junit.Test

class LyricsTest {
    @Test
    fun `timestamps follow seeks in either direction and join translations`() {
        val lines = parseLyrics(
            """
            [00:01.25][00:02.5]原文
            [00:01:250]译文
            [00:02.5]原文
            [offset:+150]
            """.trimIndent(),
        )
        assertEquals(
            listOf(LyricLine(1100, "原文\n译文"), LyricLine(2350, "原文")),
            lines,
        )
        assertEquals(listOf(-1, 0, 0, 1, 0, -1), listOf(0L, 1100L, 2349L, 2350L, 1200L, 0L).map { lineIndex(lines, it) })
    }

    @Test
    fun `an interlude is known from an empty timed line, never guessed`() {
        val lines = parseLyrics("[00:06.00]第一句\n[00:09.00]\n[00:20.00]第二句\n[00:22.00]第三句")
        assertEquals(listOf(9000L, -1L, -1L), lines.map { it.endMs })
        assertEquals(3000L, spanOf(lines, 0))
        assertEquals(LyricRest(0, 0, 6000), restAt(lines, -1, 1000))
        assertEquals(null, restAt(lines, 0, 8000))
        assertEquals(LyricRest(1, 9000, 20000), restAt(lines, 0, 12000))
        assertEquals(null, restAt(lines, 1, 21000))
    }

    @Test
    fun wordTimingsAndShortSecondsReadLikeTheWeb() {
        val lines = parseLyrics("[00:5.00]<00:05.00>逐<00:05.40>字\n[00:07.00]")
        assertEquals(listOf(LyricLine(5000, "逐字", 7000)), lines)
    }
}
