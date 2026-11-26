package com.trackferry.tv

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MotionTest {
    @Test fun smoothFramesKeepTheMotion() {
        Motion.reset()
        repeat(1000) { Motion.frame(16_700_000L) }
        assertFalse(Motion.calm.value)
    }

    @Test fun aPauseIsNotSlowness() {
        Motion.reset()
        repeat(5) { repeat(100) { Motion.frame(16_700_000L) }; Motion.frame(2_000_000_000L) }
        assertFalse(Motion.calm.value)
    }

    @Test fun sustainedSlowFramesCalmTheStage() {
        Motion.reset()
        repeat(240) { Motion.frame(40_000_000L) }
        assertTrue(Motion.calm.value)
        repeat(1000) { Motion.frame(16_700_000L) }
        assertTrue("stays calm for the run", Motion.calm.value)
        Motion.reset()
    }
}
