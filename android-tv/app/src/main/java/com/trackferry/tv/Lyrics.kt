package com.trackferry.tv

/** endMs: when the line is over, if the LRC marks it with an empty timed line; -1 when it does not say. */
internal data class LyricLine(val atMs: Long, val text: String, val endMs: Long = -1)
/** An interlude: the music is heading for line [next], from [fromMs] to [toMs]. */
internal data class LyricRest(val next: Int, val fromMs: Long, val toMs: Long)

private val timeTag = Regex("""\[(\d{1,4}):([0-5]?\d)(?:[.:](\d{1,3}))?]""")
// Word timings of enhanced LRC and tags such as [ar:…] inside a line are not words.
private val inlineTag = Regex("""<\d+:[\d.:]+>|\[[a-zA-Z]+:[^\]]*]""")
private val offsetTag = Regex("""\[offset:([+-]?\d+)]""", RegexOption.IGNORE_CASE)

internal fun parseLyrics(text: String): List<LyricLine> {
    val offset = offsetTag.findAll(text).lastOrNull()?.groupValues?.get(1)?.toLongOrNull()
        ?.coerceIn(-60_000, 60_000) ?: 0
    val groups = sortedMapOf<Long, MutableSet<String>>()
    val ends = sortedSetOf<Long>()
    for (line in text.lineSequence()) {
        val tags = timeTag.findAll(line).toList()
        if (tags.isEmpty()) continue
        val words = line.substring(tags.last().range.last + 1).replace(inlineTag, "").trim()
        for (tag in tags) {
            val (_, minutes, seconds, fraction) = tag.groupValues
            val ms = minutes.toLong() * 60_000 + seconds.toLong() * 1_000 +
                fraction.padEnd(3, '0').toLong() - offset
            // An empty timed line marks where the singing stops, so an interlude is known, not guessed.
            if (words.isEmpty()) ends += ms else groups.getOrPut(ms) { linkedSetOf() }.add(words)
        }
    }
    val starts = groups.keys.toList()
    return groups.entries.mapIndexed { i, (ms, words) ->
        val next = starts.getOrNull(i + 1) ?: Long.MAX_VALUE
        val end = ends.higher(ms)?.takeIf { it < next } ?: -1
        LyricLine(ms, words.joinToString("\n"), end)
    }
}

/** How long line [i] is sung: to its marked end, else to the next line, never through a long interlude. */
internal fun spanOf(lines: List<LyricLine>, i: Int): Long {
    val line = lines[i]
    if (line.endMs >= 0) return (line.endMs - line.atMs).coerceAtLeast(400)
    val next = lines.getOrNull(i + 1)?.atMs ?: (line.atMs + 4_000)
    return (next - line.atMs).coerceIn(400L, 600L + line.text.length * 420L)
}

/** The interlude at [atMs], if any: before the first line, or after a line marked as over. */
internal fun restAt(lines: List<LyricLine>, index: Int, atMs: Long): LyricRest? {
    val rest = 4_000L
    if (index < 0) return lines.firstOrNull()?.takeIf { it.atMs >= rest }?.let { LyricRest(0, 0, it.atMs) }
    val line = lines.getOrNull(index) ?: return null
    val next = lines.getOrNull(index + 1) ?: return null
    if (line.endMs < 0 || atMs < line.endMs || next.atMs - line.endMs < rest) return null
    return LyricRest(index + 1, line.endMs, next.atMs)
}

internal fun lineIndex(lines: List<LyricLine>, positionMs: Long): Int {
    val index = lines.binarySearchBy(positionMs) { it.atMs }
    return if (index >= 0) index else -index - 2
}
