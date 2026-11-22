package com.louis.myplayer

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.net.InetSocketAddress
import java.net.ServerSocket
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.TimeUnit

/**
 * 「手机连电脑」 on the JVM: a fake PC (ServerSocket) speaks the reverse protocol
 * (shiyin-sync-protocol.md「反向连接」) to the real ReverseClient + SyncServer.serveReverse.
 */
class ReverseClientTest {
    private val servers = mutableListOf<ServerSocket>()
    @After fun tearDown() { ReverseClient.reset(); servers.forEach { it.close() } }

    private fun readLine(i: java.io.InputStream): String {
        val b = ByteArrayOutputStream()
        while (true) { val c = i.read(); if (c < 0 || c == '\n'.code) break; b.write(c) }
        return b.toString("UTF-8").trimEnd('\r')
    }

    /** Fake PC: checks the handshake, sends keep-alive blank lines, then one HTTP request per connection. */
    private fun fakePc(code: String, requests: List<String>, hellos: ArrayBlockingQueue<String>, responses: ArrayBlockingQueue<String>): Int {
        val ss = ServerSocket(); ss.bind(InetSocketAddress("127.0.0.1", 0)); servers += ss
        Thread {
            var n = 0
            while (!ss.isClosed) {
                val s = try { ss.accept() } catch (_: Exception) { break }
                val hello = readLine(s.getInputStream())
                hellos.offer(hello)
                val parts = hello.split(' ', limit = 3)
                if (parts.getOrNull(1) != code) { s.getOutputStream().write("ERR badcode 配对码不对\n".toByteArray()); s.close(); continue }
                s.getOutputStream().write("OK {\"app\":\"shiyin-pc\",\"name\":\"DESKTOP-TEST\"}\n".toByteArray())
                val path = requests.getOrNull(n++)
                if (path == null) { continue } // keep it idle in the "pool"
                Thread {
                    s.getOutputStream().write("\r\n\r\n".toByteArray()) // keep-alives before the request
                    Thread.sleep(50)
                    s.getOutputStream().write("GET $path?code=000000 HTTP/1.1\r\nHost: shiyin-phone\r\nConnection: close\r\n\r\n".toByteArray())
                    responses.offer(String(s.getInputStream().readBytes(), Charsets.UTF_8))
                    s.close()
                }.start()
            }
        }.start()
        return ss.localPort
    }

    @Test fun handshakeAndServeOverOutboundConnection() {
        val hellos = ArrayBlockingQueue<String>(32); val resp = ArrayBlockingQueue<String>(8)
        val port = fakePc("654321", listOf("/api/ping", "/api/library"), hellos, resp)
        val r = ReverseClient.connect("127.0.0.1:$port", "654321")
        assertTrue(r.toString(), r.getBoolean("ok"))
        assertEquals("DESKTOP-TEST", r.getString("pc"))
        val hello = hellos.poll(5, TimeUnit.SECONDS)!!
        assertTrue(hello, hello.startsWith("SHIYIN-REVERSE/1 654321 {"))
        assertTrue(hello, hello.contains("\"app\":\"shiyin\""))
        val got = listOf(resp.poll(5, TimeUnit.SECONDS)!!, resp.poll(5, TimeUnit.SECONDS)!!) // the two requests run in parallel
        val ping = got.first { it.contains("needCode") }
        val lib = got.first { it.contains("\"tracks\"") }
        assertTrue(ping, ping.startsWith("HTTP/1.1 200"))
        assertTrue(ping, ping.contains("\"app\":\"shiyin\"") && ping.contains("\"needCode\":false"))
        // no pairing code needed on the reverse path (the PC was authenticated in the handshake)
        assertTrue(lib, lib.startsWith("HTTP/1.1 200") && lib.contains("\"tracks\":["))
        assertEquals("connected", ReverseClient.state)
        // pool refills: more than the first SLOTS connections were opened
        Thread.sleep(300)
        assertTrue("hellos=${hellos.size}", hellos.size >= 3)
    }

    @Test fun wrongCodeIsReported() {
        val port = fakePc("111111", emptyList(), ArrayBlockingQueue(8), ArrayBlockingQueue(8))
        val r = ReverseClient.connect("127.0.0.1:$port", "222222")
        assertFalse(r.getBoolean("ok"))
        assertTrue(r.getString("msg"), r.getString("msg").contains("配对码不对"))
        assertEquals("error", ReverseClient.state)
    }

    @Test fun unreachablePcAndBadInput() {
        val r = ReverseClient.connect("127.0.0.1:1", "123456")
        assertFalse(r.getBoolean("ok"))
        assertTrue(r.getString("msg"), r.getString("msg").contains("拒绝连接") && r.getString("msg").contains("从手机导入"))
        val r2 = ReverseClient.connect("127.0.0.1", "12")
        assertTrue(r2.getString("msg").contains("6 位"))
    }

    @Test fun candidatesSkipLoopback() {
        for (c in SyncServer.candidates()) assertFalse(c.toString(), c.getString("ip").startsWith("127."))
    }
}
