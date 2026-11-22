package com.louis.myplayer

import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File

/**
 * Manual cross-implementation test against the real PC hub (Go):
 *   SHIYIN_INTEROP=/tmp/hub go test -run Interop .        (in shiyin-pc, starts the hub)
 *   SHIYIN_INTEROP=/tmp/hub gradle :app:testReleaseUnitTest  (this test connects with the real ReverseClient)
 */
class InteropTest {
    @Test fun realPcHub() {
        val base = System.getenv("SHIYIN_INTEROP")
        assumeTrue(base != null)
        val info = File("$base.txt")
        val until = System.currentTimeMillis() + 60_000
        while (!info.exists() && System.currentTimeMillis() < until) Thread.sleep(200)
        val (port, code) = info.readText().trim().split(' ')
        val r = ReverseClient.connect("127.0.0.1:$port", code)
        assertTrue(r.toString(), r.getBoolean("ok"))
        val res = File("$base.result")
        val until2 = System.currentTimeMillis() + 60_000
        while (!res.exists() && System.currentTimeMillis() < until2) Thread.sleep(200)
        ReverseClient.reset()
        val txt = res.readText()
        assertTrue(txt, txt.contains("\"app\":\"shiyin\"") && txt.contains("/api/file?key=nope 404"))
    }
}
