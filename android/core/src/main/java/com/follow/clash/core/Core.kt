package com.follow.clash.core

import com.follow.clash.core.mobile.Mobile
import com.follow.clash.core.mobile.ResultHandler
import com.follow.clash.core.mobile.TunHandler
import com.follow.clash.common.GlobalState
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.URI
import android.system.Os
import android.util.AtomicFile

object Core {
    @Synchronized
    private fun prepareNetBirdStorage(initParams: JSONObject) {
        val home = initParams.getString("home-dir")
        val directoryName = MessageDigest.getInstance("SHA-256").digest(home.toByteArray())
            .joinToString("") { "%02x".format(it) }
        val directory = File(GlobalState.application.noBackupFilesDir, "netbird/$directoryName")
        check(Mobile.setNetBirdDirectory(directory.absolutePath)) { "Cannot change active NetBird storage" }
        val settings = File(directory, "settings.json")
        val assets = GlobalState.application.assets
        if (!settings.exists() && assets.list("").orEmpty().contains("netbird-provision.json")) {
            val data = assets.open("netbird-provision.json").use { it.readBytes() }
            require(data.size <= 4096) { "Invalid NetBird provisioning size" }
            JSONObject(String(data, Charsets.UTF_8))
            check(directory.isDirectory || directory.mkdirs()) { "Cannot create private NetBird storage" }
            Os.chmod(directory.path, 448)
            val atomic = AtomicFile(settings)
            val stream = atomic.startWrite()
            try {
                Os.fchmod(stream.fd, 384)
                stream.write(data)
                atomic.finishWrite(stream)
            } catch (error: Exception) {
                atomic.failWrite(stream)
                throw error
            }
        }
    }

    fun forceGC() = Mobile.forceGC()

    fun updateDNS(
        dns: String,
    ) = Mobile.updateDns(dns)

    fun resetNetwork() = Mobile.resetNetwork()

    fun setHomeWifiMode(mode: String) = Mobile.setHomeWifiMode(mode)

    private fun parseInetSocketAddress(address: String): InetSocketAddress {
        val uri = URI("tcp://$address")
        val host = requireNotNull(uri.host) { "Missing host in address: $address" }
        require(uri.port >= 0) { "Missing port in address: $address" }
        return InetSocketAddress(InetAddress.getByName(host), uri.port)
    }

    fun startTun(
        fd: Int,
        protect: (Int) -> Boolean,
        discoverInterfaces: () -> String,
        resolveUid: (protocol: Int, source: InetSocketAddress, target: InetSocketAddress) -> Int,
        resolvePackage: (uid: Int) -> String,
        stack: String,
        address: String,
        dns: String,
        options: String,
    ): Boolean {
        return Mobile.startTun(
            fd,
            object : TunHandler {
                override fun protect(fd: Int): Boolean = protect(fd)

                override fun interfaces(): String = discoverInterfaces()

                override fun resolveUid(
                    protocol: Int,
                    source: String,
                    target: String,
                ): Int {
                    return resolveUid(
                        protocol,
                        parseInetSocketAddress(source),
                        parseInetSocketAddress(target),
                    )
                }

                override fun resolvePackage(uid: Int): String = resolvePackage(uid)
            },
            stack,
            address,
            dns,
            options,
        )
    }

    fun invokeMethod(
        data: String,
        cb: (result: ByteArray?) -> Unit,
    ) {
        val call = JSONObject(data)
        if (call.optString("method") == "initClash") {
            prepareNetBirdStorage(call.getJSONObject("arguments"))
        }
        Mobile.invokeMethod(data, ResultHandler { result -> cb(result) })
    }

    fun updateEventListener(
        callback: ((result: ByteArray) -> Unit)?,
    ) {
        Mobile.setEventListener(
            callback?.let { ResultHandler { result -> it(result ?: ByteArray(0)) } },
        )
    }

    fun quickSetup(
        initParamsString: String,
        setupParamsString: String,
        callback: (result: String?) -> Unit,
    ) {
        prepareNetBirdStorage(JSONObject(initParamsString))
        Mobile.quickSetup(
            initParamsString,
            setupParamsString,
            ResultHandler { result -> callback(result?.decodeToString()) },
        )
    }

    fun netBirdCIDR(): String = Mobile.netBirdCIDR()

    fun stopTun() = Mobile.stopTun()

    fun startListener() = Mobile.startListener()

    fun stopListener() = Mobile.stopListener()

    fun getTraffic(onlyStatisticsProxy: Boolean): String = Mobile.getTraffic(onlyStatisticsProxy)

    fun getTotalTraffic(onlyStatisticsProxy: Boolean): String = Mobile.getTotalTraffic(onlyStatisticsProxy)
}
