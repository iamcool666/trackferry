package com.trackferry.tv

internal data class LyricLine(val atMs: Long, val text: String)

private val timeTag = Regex("""\[(\d{1,4}):([0-5]\d)(?:[.:](\d{1,3}))?]""")
private val offsetTag = Regex("""\[offset:([+-]?\d+)]""", RegexOption.IGNORE_CASE)

internal fun parseLyrics(text: String): List<LyricLine> {
    val offset = offsetTag.findAll(text).lastOrNull()?.groupValues?.get(1)?.toLongOrNull()
        ?.coerceIn(-60_000, 60_000) ?: 0
    val groups = sortedMapOf<Long, MutableSet<String>>()
    for (line in text.lineSequence()) {
        val tags = timeTag.findAll(line).toList()
        if (tags.isEmpty()) continue
        val words = line.substring(tags.last().range.last + 1).trim()
        if (words.isEmpty()) continue
        for (tag in tags) {
            val (_, minutes, seconds, fraction) = tag.groupValues
            val ms = minutes.toLong() * 60_000 + seconds.toLong() * 1_000 +
                fraction.padEnd(3, '0').toLong() - offset
            groups.getOrPut(ms) { linkedSetOf() }.add(words)
        }
    }
    return groups.map { (ms, words) -> LyricLine(ms, words.joinToString("\n")) }
}

internal fun lineIndex(lines: List<LyricLine>, positionMs: Long): Int {
    val index = lines.binarySearchBy(positionMs) { it.atMs }
    return if (index >= 0) index else -index - 2
}
