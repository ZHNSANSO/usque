package com.zhnsanso.usque

import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.os.Bundle
import android.util.Log
import android.view.LayoutInflater
import android.widget.*
import usqueandroid.Usqueandroid

class MainActivity : Activity() {

    companion object {
        private const val VPN_REQUEST_CODE = 1001
        private const val PREFS_NAME = "UsquePrefs"
        private const val TAG = "MainActivity"
    }

    private lateinit var prefs: SharedPreferences
    private lateinit var statusText: TextView
    private lateinit var ipInfoText: TextView
    private lateinit var sniText: TextView
    private lateinit var endpointText: TextView
    private lateinit var connectButton: Button
    private lateinit var settingsButton: Button

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        prefs = getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

        statusText = findViewById(R.id.status_text)
        ipInfoText = findViewById(R.id.ip_info_text)
        sniText = findViewById(R.id.sni_text)
        endpointText = findViewById(R.id.endpoint_text)
        connectButton = findViewById(R.id.connect_button)
        settingsButton = findViewById(R.id.settings_button)

        connectButton.setOnClickListener {
            if (UsqVpnService.isRunning) {
                stopVpn()
            } else {
                startVpn()
            }
        }

        settingsButton.setOnClickListener {
            showSettingsDialog()
        }

        // Initialize display
        updateUI()
    }

    override fun onResume() {
        super.onResume()
        updateUI()
    }

    private fun startVpn() {
        val intent = VpnService.prepare(this)
        if (intent != null) {
            startActivityForResult(intent, VPN_REQUEST_CODE)
        } else {
            onVpnPermissionGranted()
        }
    }

    private fun stopVpn() {
        UsqVpnService.stop()
        // Immediate UI feedback
        updateUIState(false)
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        if (requestCode == VPN_REQUEST_CODE && resultCode == RESULT_OK) {
            onVpnPermissionGranted()
        }
    }

    private fun onVpnPermissionGranted() {
        startService(Intent(this, UsqVpnService::class.java))
        updateUIState(true)
    }

    private fun updateUI() {
        val configPath = "${filesDir.absolutePath}/config.json"
        val running = UsqVpnService.isRunning
        
        updateUIState(running)

        if (Usqueandroid.isRegistered(configPath)) {
            val ipv4 = Usqueandroid.getAssignedIPv4(configPath)
            val ipv6 = Usqueandroid.getAssignedIPv6(configPath)
            ipInfoText.text = "IPv4: $ipv4\nIPv6: $ipv6"
        } else {
            ipInfoText.text = "Not Registered"
        }

        // Load current live settings
        sniText.text = "SNI: ${prefs.getString("sni", "www.visa.com.cn")}"
        endpointText.text = "Endpoint: ${prefs.getString("endpoint", "Auto")}"
    }

    private fun updateUIState(running: Boolean) {
        if (running) {
            statusText.text = "Connected"
            statusText.setTextColor(getColor(android.R.color.holo_green_dark))
            connectButton.text = "Disconnect"
            settingsButton.isEnabled = false
        } else {
            statusText.text = "Disconnected"
            statusText.setTextColor(getColor(android.R.color.holo_red_dark))
            connectButton.text = "Connect"
            settingsButton.isEnabled = true
        }
    }

    private fun showSettingsDialog() {
        val dialogView = LayoutInflater.from(this).inflate(R.layout.dialog_settings, null)
        val sniInput = dialogView.findViewById<EditText>(R.id.sni_input)
        val endpointInput = dialogView.findViewById<EditText>(R.id.endpoint_input)

        // Pre-fill
        sniInput.setText(prefs.getString("sni", ""))
        endpointInput.setText(prefs.getString("endpoint", ""))

        AlertDialog.Builder(this)
            .setTitle("Connection Settings")
            .setView(dialogView)
            .setPositiveButton("Save") { _, _ ->
                val sni = sniInput.text.toString()
                val endpoint = endpointInput.text.toString()

                prefs.edit().apply {
                    putString("sni", sni)
                    putString("endpoint", endpoint)
                    apply()
                }

                // Apply to JNI immediately
                Usqueandroid.setSNI(sni)
                Usqueandroid.setEndpoint(endpoint)
                
                Toast.makeText(this, "Settings updated", Toast.LENGTH_SHORT).show()
                updateUI()
            }
            .setNegativeButton("Cancel", null)
            .setNeutralButton("Reset") { _, _ ->
                Usqueandroid.resetConnectionOptions()
                prefs.edit().clear().apply()
                updateUI()
            }
            .show()
    }
}
