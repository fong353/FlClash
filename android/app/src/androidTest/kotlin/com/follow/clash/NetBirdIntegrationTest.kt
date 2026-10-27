package com.follow.clash

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.IBinder
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.follow.clash.common.AccessControlMode
import com.follow.clash.core.Core
import com.follow.clash.service.NetworkObserver
import com.follow.clash.service.ServiceConfig
import com.follow.clash.service.VpnService
import com.follow.clash.service.models.AccessControlProps
import com.follow.clash.service.models.VpnOptions
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.security.MessageDigest
import java.io.File
import java.net.Socket
import java.net.InetSocketAddress
import java.net.DatagramSocket
import java.net.DatagramPacket
import java.net.InetAddress
import java.net.URL
import javax.net.ssl.HttpsURLConnection
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

@RunWith(AndroidJUnit4::class)
class NetBirdIntegrationTest {
    private fun rpc(method: String, arguments: Any? = null): JSONObject {
        val done = CountDownLatch(1)
        var answer: JSONObject? = null
        val call = JSONObject().put("id", "instrumentation").put("method", method)
        if (arguments != null) call.put("arguments", arguments)
        Core.invokeMethod(call.toString()) { data ->
            answer = JSONObject(String(requireNotNull(data)))
            done.countDown()
        }
        assertTrue("Core RPC timed out: $method", done.await(20, TimeUnit.SECONDS))
        val response = requireNotNull(answer)
        assertFalse("Core RPC failed: $method: $response", response.has("error"))
        return response
    }

    private fun awaitState(state: String) {
        val until = System.nanoTime() + TimeUnit.SECONDS.toNanos(8)
        while (System.nanoTime() < until) {
            if (rpc("getNetBirdStatus").getJSONObject("result").getString("state") == state) return
            Thread.sleep(100)
        }
        fail("NetBird did not reach $state")
    }

    @Test
    fun oneVpnOwnsOverlayRoutesAndStopsPendingEnrollment() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val home = File(context.filesDir, "netbird-instrumentation").apply { mkdirs() }
        File(home, "config.yaml").writeText("mode: rule\nmixed-port: 17891\nallow-lan: false\nbind-address: 127.0.0.1\nrules:\n  - MATCH,DIRECT\ndns:\n  enable: false\n")
        rpc("initClash", JSONObject().put("home-dir", home.path).put("version", android.os.Build.VERSION.SDK_INT))
        rpc("configureNetBird", JSONObject()
            .put("enabled", true)
            .put("managementUrl", "https://127.0.0.1:9")
            .put("deviceName", "isolated-android-test")
            .put("cidr", "10.10.10.0/24")
            .put("setupKey", "unused-instrumentation-key"))
        rpc("setupConfig", JSONObject().put("selected-map", JSONObject()).put("test-url", "https://example.com"))
        val status = rpc("getNetBirdStatus").getJSONObject("result")
        assertFalse("setup key must never cross the status API", status.has("setupKey"))
        assertEquals("10.10.10.0/24", Core.netBirdCIDR())
        assertNull("isolated emulator must pre-authorize this VPN", android.net.VpnService.prepare(context))
        NetworkObserver.start()
        ServiceConfig.updateVpnOptions(VpnOptions(
            enable = true, port = 17891, ipv6 = false, dnsHijacking = false,
            accessControlProps = AccessControlProps(false, AccessControlMode.REJECT_SELECTED, emptyList(), emptyList()),
            allowBypass = false, systemProxy = true, bypassDomain = emptyList(),
            stack = "gvisor", routeAddress = listOf("10.10.10.0/24"), mtu = 1500,
        ))
        val bound = CountDownLatch(1)
        var service: VpnService? = null
        val connection = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
                service = (binder as VpnService.LocalBinder).service
                bound.countDown()
            }
            override fun onServiceDisconnected(name: ComponentName?) { service = null }
        }
        assertTrue(context.bindService(Intent(context, VpnService::class.java), connection, Context.BIND_AUTO_CREATE))
        assertTrue(bound.await(10, TimeUnit.SECONDS))
        try {
            repeat(3) {
                requireNotNull(service).start()
                val connectivity = context.getSystemService(ConnectivityManager::class.java)
                val until = System.nanoTime() + TimeUnit.SECONDS.toNanos(5)
                var found = false
                while (System.nanoTime() < until) {
                    val network = connectivity.allNetworks.firstOrNull {
                        connectivity.getNetworkCapabilities(it)?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true
                    }
                    if (network != null) {
                        val properties = requireNotNull(connectivity.getLinkProperties(network))
                        assertNull("HTTP proxy must not bypass the overlay TUN route", properties.httpProxy)
                        assertTrue(properties.routes.any { it.destination.toString() == "10.10.10.0/24" })
                        found = true
                        break
                    }
                    Thread.sleep(100)
                }
                assertTrue("VPN transport was not established", found)
                val before = System.nanoTime()
                requireNotNull(service).stop()
                assertTrue("VPN teardown blocked on authentication", System.nanoTime() - before < TimeUnit.SECONDS.toNanos(2))
                awaitState("stopped")
            }
        } finally {
            service?.stop()
            context.unbindService(connection)
            Core.stopListener()
            rpc("shutdown")
        }
    }

    private fun awaitConnected(): JSONObject {
        val until = System.nanoTime() + TimeUnit.SECONDS.toNanos(60)
        var status = JSONObject()
        while (System.nanoTime() < until) {
            status = rpc("getNetBirdStatus").getJSONObject("result")
            if (status.optBoolean("management") && status.optBoolean("signal")) return status
            if (status.optString("state") == "error") fail("NetBird connection failed: ${status.optString("error")}")
            Thread.sleep(200)
        }
        fail("NetBird did not connect: $status")
        return status
    }

    private fun checkOverlay(fixture: JSONObject, expectedSource: String) {
        Socket().use { socket ->
            socket.soTimeout = 15000
            socket.connect(InetSocketAddress(fixture.getString("targetIP"), fixture.getInt("httpPort")), 15000)
            socket.getOutputStream().write("GET / HTTP/1.0\r\nHost: overlay-test\r\nConnection: close\r\n\r\n".toByteArray())
            val body = socket.getInputStream().bufferedReader().readText().substringAfter("\r\n\r\n")
            val result = JSONObject(body)
            assertEquals(fixture.getString("nonce"), result.getString("nonce"))
            assertEquals("TCP must originate from the embedded peer", expectedSource, result.getString("source"))
        }
        DatagramSocket().use { socket ->
            socket.soTimeout = 15000
            val payload = fixture.getString("nonce").toByteArray()
            socket.send(DatagramPacket(payload, payload.size, InetAddress.getByName(fixture.getString("targetIP")), fixture.getInt("udpPort")))
            val packet = DatagramPacket(ByteArray(4096), 4096)
            socket.receive(packet)
            val result = JSONObject(String(packet.data, 0, packet.length))
            assertEquals(fixture.getString("nonce"), result.getString("nonce"))
            assertEquals("UDP must originate from the embedded peer", expectedSource, result.getString("source"))
        }
    }

    @Test
    fun liveProxyAndOverlayShareVpn() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val fixtureFile = File(context.filesDir, "netbird-live-test.json")
        assumeTrue("private live-test fixture is not installed", fixtureFile.isFile)
        val fixture = JSONObject(fixtureFile.readText())
        fixtureFile.delete()
        val home = File(context.filesDir, "netbird-live-instrumentation").apply { mkdirs() }
        File(home, "config.yaml").writeText(fixture.getString("yaml"))
        rpc("initClash", JSONObject().put("home-dir", home.path).put("version", android.os.Build.VERSION.SDK_INT))
        rpc("configureNetBird", JSONObject()
            .put("enabled", true)
            .put("managementUrl", fixture.getString("managementUrl"))
            .put("deviceName", fixture.getString("deviceName"))
            .put("cidr", fixture.getString("cidr"))
            .put("setupKey", fixture.getString("setupKey")))
        rpc("setupConfig", JSONObject().put("selected-map", JSONObject()).put("test-url", "https://example.com"))
        assertNull(android.net.VpnService.prepare(context))
        NetworkObserver.start()
        ServiceConfig.updateVpnOptions(VpnOptions(
            enable = true, port = 17891, ipv6 = false, dnsHijacking = true,
            accessControlProps = AccessControlProps(false, AccessControlMode.REJECT_SELECTED, emptyList(), emptyList()),
            allowBypass = false, systemProxy = true, bypassDomain = emptyList(),
            stack = "gvisor", routeAddress = emptyList(), mtu = 1500,
        ))
        val bound = CountDownLatch(1)
        var service: VpnService? = null
        val connection = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
                service = (binder as VpnService.LocalBinder).service
                bound.countDown()
            }
            override fun onServiceDisconnected(name: ComponentName?) { service = null }
        }
        assertTrue(context.bindService(Intent(context, VpnService::class.java), connection, Context.BIND_AUTO_CREATE))
        assertTrue(bound.await(10, TimeUnit.SECONDS))
        try {
            requireNotNull(service).start()
            val connected = awaitConnected()
            val source = connected.getString("ip").substringBefore('/')
            assertTrue(connected.getBoolean("enrolled"))
            val storageName = MessageDigest.getInstance("SHA-256").digest(home.path.toByteArray()).joinToString("") { "%02x".format(it) }
            val identitySettings = File(context.noBackupFilesDir, "netbird/$storageName/settings.json")
            assertFalse(identitySettings.readText().contains(fixture.getString("setupKey")))
            assertFalse(File(home, "netbird").exists())
            val identityUri = android.provider.DocumentsContract.buildDocumentUri(
                "${context.packageName}.files", identitySettings.path,
            )
            assertThrows(java.io.FileNotFoundException::class.java) {
                context.contentResolver.openFileDescriptor(identityUri, "r")?.close()
            }
            val internet = URL("https://api.ipify.org").openConnection() as HttpsURLConnection
            internet.connectTimeout = 20000
            internet.readTimeout = 20000
            try {
                assertEquals(fixture.getString("expectedEgress"), internet.inputStream.bufferedReader().readText().trim())
            } finally { internet.disconnect() }
            for (mode in listOf("rule", "global", "direct")) {
                rpc("updateConfig", JSONObject().put("mode", mode))
                checkOverlay(fixture, source)
            }
            requireNotNull(service).stop()
            awaitState("stopped")
            requireNotNull(service).start()
            val reconnected = awaitConnected()
            assertEquals(source, reconnected.getString("ip").substringBefore('/'))
            checkOverlay(fixture, source)
            val receipt = JSONObject()
                .put("overlayIP", source)
                .put("tcp", true).put("udp", true).put("publicProxy", true)
                .put("modes", "rule,global,direct").put("identityPreserved", true)
            File(context.filesDir, "netbird-live-receipt.json").writeText(receipt.toString())
        } finally {
            service?.stop()
            context.unbindService(connection)
            Core.stopListener()
            rpc("shutdown")
        }
    }
}
