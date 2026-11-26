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
}
