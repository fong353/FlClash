package com.follow.clash.service

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class HomeWifiPolicyTest {
    @Test
    fun onlyExactHomeSsidUsesDirect() {
        assertEquals("direct", homeWifiMode(true, "Home WiFi", "Home WiFi"))
        for (observed in listOf(null, "", "Other", "home wifi", "Home WiFi ")) {
            assertEquals("rule", homeWifiMode(true, "Home WiFi", observed))
        }
        assertEquals("rule", homeWifiMode(true, "", ""))
        assertEquals("", homeWifiMode(false, "Home WiFi", "Home WiFi"))
    }

    @Test
    fun androidRedactionNeverMatches() {
        for (observed in listOf(null, "", "<unknown ssid>", "0x", "\"<unknown ssid>\"")) {
            assertNull(normalizeWifiSsid(observed))
            assertEquals("rule", homeWifiMode(true, "Home WiFi", normalizeWifiSsid(observed)))
        }
        assertEquals(" Home WiFi ", normalizeWifiSsid("\" Home WiFi \""))
    }
}
