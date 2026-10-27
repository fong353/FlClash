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

object Core {
    private fun prepareNetBirdStorage(initParams: JSONObject) {
        val home = initParams.getString("home-dir")
        val directoryName = MessageDigest.getInstance("SHA-256").digest(home.toByteArray())
            .joinToString("") { "%02x".format(it) }
        val directory = File(GlobalState.application.noBackupFilesDir, "netbird/$directoryName")
        check(Mobile.setNetBirdDirectory(directory.absolutePath)) { "Cannot change active NetBird storage" }
    }

    fun forceGC() = Mobile.forceGC()

    fun updateDNS(
        dns: String,
    ) = Mobile.updateDns(dns)

    fun resetNetwork() = Mobile.resetNetwork()

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
