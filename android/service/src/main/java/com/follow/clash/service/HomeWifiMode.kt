package com.follow.clash.service

import android.Manifest
import android.app.AppOpsManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.location.LocationManager
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.wifi.WifiInfo
import android.net.wifi.WifiManager
import android.os.Build
import android.os.Handler
import android.os.HandlerThread
import androidx.core.content.ContextCompat
import androidx.core.content.getSystemService
import androidx.core.location.LocationManagerCompat
import com.follow.clash.common.GlobalState
import com.follow.clash.core.Core
import java.util.concurrent.CopyOnWriteArraySet

object HomeWifiMode {
    private val context get() = GlobalState.application
    private val preferences by lazy { context.getSharedPreferences("home_wifi_mode", Context.MODE_PRIVATE) }
    private val connectivity by lazy { requireNotNull(context.getSystemService<ConnectivityManager>()) }
    private val handler by lazy { Handler(HandlerThread("HomeWifiMode").apply { start() }.looper) }
    private val listeners = CopyOnWriteArraySet<(Map<String, Any?>) -> Unit>()
    private val wifiNetworks = mutableMapOf<Network, String?>()
    private var started = false
    private var enabled = false
    private var target = ""
    private var mode = ""
    private var revision = 0L
    private var callback: WifiCallback? = null
    private var permitted = false
    private var registered = false

    private val locationReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            handler.post { refreshObservation() }
        }
    }

    private val permissionListener = AppOpsManager.OnOpChangedListener { _, packageName ->
        if (packageName == context.packageName) handler.post { refreshObservation() }
    }

    @Synchronized
    fun start() {
        if (started) return
        enabled = preferences.getBoolean("enabled", false)
        target = preferences.getString("ssid", "").orEmpty()
        Core.setHomeWifiMode(homeWifiMode(enabled, target, null))
        started = true
        handler.post { refreshObservation() }
    }

    fun addListener(listener: (Map<String, Any?>) -> Unit) { listeners.add(listener) }
    fun removeListener(listener: (Map<String, Any?>) -> Unit) { listeners.remove(listener) }

    fun getState(deliver: (Map<String, Any?>) -> Unit) {
        start()
        handler.post {
            refreshObservation()
            deliver(snapshot())
        }
    }

    fun configure(nextEnabled: Boolean, ssid: String, deliver: (Map<String, Any?>) -> Unit) {
        start()
        handler.post {
            val saved = preferences.edit().putBoolean("enabled", nextEnabled).putString("ssid", ssid).commit()
            if (!saved) {
                deliver(snapshot() + ("error" to "Unable to save Wi-Fi settings"))
                return@post
            }
            enabled = nextEnabled
            target = ssid
            refreshObservation()
            deliver(snapshot())
        }
    }

    private fun canReadSsid(): Boolean =
        ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED &&
            (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q ||
                ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_BACKGROUND_LOCATION) == PackageManager.PERMISSION_GRANTED) &&
            context.getSystemService<LocationManager>()?.let(LocationManagerCompat::isLocationEnabled) == true

    private fun refreshObservation() {
        if (enabled && !registered) {
            ContextCompat.registerReceiver(context, locationReceiver, IntentFilter(LocationManager.MODE_CHANGED_ACTION), ContextCompat.RECEIVER_NOT_EXPORTED)
            context.getSystemService<AppOpsManager>()?.startWatchingMode(AppOpsManager.OPSTR_FINE_LOCATION, context.packageName, permissionListener)
            registered = true
        } else if (!enabled && registered) {
            context.unregisterReceiver(locationReceiver)
            context.getSystemService<AppOpsManager>()?.stopWatchingMode(permissionListener)
            registered = false
        }
        val nextPermitted = enabled && canReadSsid()
        if (!nextPermitted || nextPermitted != permitted) {
            callback?.let { runCatching { connectivity.unregisterNetworkCallback(it) } }
            callback = null
            wifiNetworks.clear()
        }
        permitted = nextPermitted
        if (permitted && callback == null) {
            val next = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) WifiCallback(ConnectivityManager.NetworkCallback.FLAG_INCLUDE_LOCATION_INFO) else WifiCallback()
            val request = NetworkRequest.Builder()
                .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
                .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
                .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                .build()
            runCatching { connectivity.registerNetworkCallback(request, next, handler) }
                .onSuccess { callback = next }
                .onFailure { GlobalState.log("Unable to observe home Wi-Fi: ${it.javaClass.simpleName}") }
        }
        publish()
    }

    private fun observedSsid(): String? = if (enabled && canReadSsid()) wifiNetworks.values.firstOrNull() else null

    private fun snapshot(): Map<String, Any?> = mapOf(
        "enabled" to enabled,
        "ssid" to target,
        "observedSsid" to observedSsid(),
        "mode" to mode,
        "revision" to revision,
        "authorized" to canReadSsid(),
    )

    private fun publish() {
        val next = homeWifiMode(enabled, target, observedSsid())
        if (next != mode) {
            Core.setHomeWifiMode(next)
            mode = next
            GlobalState.log("Home Wi-Fi automatic mode: ${mode.ifEmpty { "disabled" }}")
        }
        revision++
        val state = snapshot()
        listeners.forEach { it(state) }
    }

    private class WifiCallback : ConnectivityManager.NetworkCallback {
        constructor() : super()
        constructor(flags: Int) : super(flags)

        override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
            if (callback !== this) return
            val ssid = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                (capabilities.transportInfo as? WifiInfo)?.ssid
            } else {
                @Suppress("DEPRECATION")
                context.getSystemService<WifiManager>()?.connectionInfo?.ssid
            }
            wifiNetworks[network] = normalizeWifiSsid(ssid)
            publish()
        }

        override fun onLost(network: Network) {
            if (callback !== this) return
            wifiNetworks.remove(network)
            publish()
        }
    }
}
