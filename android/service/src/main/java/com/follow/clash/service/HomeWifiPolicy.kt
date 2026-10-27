package com.follow.clash.service

internal fun normalizeWifiSsid(value: String?): String? = value
    ?.removeSurrounding("\"")
    ?.takeUnless { it.isEmpty() || it == "<unknown ssid>" || it == "0x" }

internal fun homeWifiMode(enabled: Boolean, target: String, observed: String?): String = when {
    !enabled -> ""
    target.isNotEmpty() && target == observed -> "direct"
    else -> "rule"
}
