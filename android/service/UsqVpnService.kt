package com.zhnsanso.usque

import android.content.Intent
import android.net.VpnService
import android.os.ParcelFileDescriptor
import android.util.Log
import usqueandroid.PacketFlow
import usqueandroid.Usqueandroid
import usqueandroid.VpnStateCallback
import java.io.FileOutputStream

/**
 * UsqVpnService manages the system-level VPN lifecycle on Android.
 */
class UsqVpnService : VpnService() {

    companion object {
        private const val TAG = "UsqVpnService"
        const val ACTION_DISCONNECT = "com.zhnsanso.usque.DISCONNECT"
        
        var isRunning = false
            private set
            
        private var instance: UsqVpnService? = null
        
        fun stop() {
            instance?.disconnect()
        }
    }

    private var vpnInterface: ParcelFileDescriptor? = null
    private var outputStream: FileOutputStream? = null

    override fun onCreate() {
        super.onCreate()
        instance = this
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_DISCONNECT) {
            disconnect()
            return START_NOT_STICKY
        }

        if (isRunning) {
            return START_STICKY
        }

        val configPath = "${filesDir.absolutePath}/config.json"

        // 1. Check Registration
        if (!Usqueandroid.isRegistered(configPath)) {
            Log.e(TAG, "Device not registered")
            stopSelf()
            return START_NOT_STICKY
        }

        // 2. Get assigned addresses
        val vpnIpv4 = Usqueandroid.getAssignedIPv4(configPath)
        val vpnIpv6 = Usqueandroid.getAssignedIPv6(configPath)

        if (vpnIpv4.isEmpty()) {
            Log.e(TAG, "No IP address assigned")
            stopSelf()
            return START_NOT_STICKY
        }

        // 3. Establish VPN Interface
        try {
            val builder = Builder()
                .setSession("Usque")
                .setMtu(1280)
                .addAddress(vpnIpv4, 32)
                .addRoute("0.0.0.0", 0)
                .addDnsServer("1.1.1.1")
                .addDnsServer("1.0.0.1")
                // IPv6 Dual Stack
                if (vpnIpv6.isNotEmpty()) {
                    builder.addAddress(vpnIpv6, 128)
                    builder.addRoute("::", 0)
                    builder.addDnsServer("2606:4700:4700::1111")
                }
                // Bypass self to avoid loop
                builder.addDisallowedApplication(packageName)

            vpnInterface = builder.establish()
            if (vpnInterface == null) {
                stopSelf()
                return START_NOT_STICKY
            }

            outputStream = FileOutputStream(vpnInterface!!.fileDescriptor)
            isRunning = true

            // 4. Start Go Tunnel
            val packetFlow = object : PacketFlow {
                override fun writePacket(data: ByteArray?) {
                    if (data != null && data.isNotEmpty()) {
                        try {
                            outputStream?.write(data)
                        } catch (e: Exception) {
                            Log.e(TAG, "Failed to write packet", e)
                        }
                    }
                }
            }

            val callback = object : VpnStateCallback {
                override fun onConnected() {
                    Log.i(TAG, "Tunnel connected to Cloudflare")
                }
                override fun onDisconnected(reason: String?) {
                    Log.w(TAG, "Tunnel disconnected: $reason")
                    disconnect()
                }
                override fun onError(message: String?) {
                    Log.e(TAG, "Tunnel error: $message")
                }
            }

            val error = Usqueandroid.startTunnel(configPath, vpnInterface!!.fd, 1280, packetFlow, callback)
            if (error.isNotEmpty()) {
                Log.e(TAG, "Go core failed: $error")
                disconnect()
                return START_NOT_STICKY
            }

        } catch (e: Exception) {
            Log.e(TAG, "Setup failed", e)
            stopSelf()
            return START_NOT_STICKY
        }

        return START_STICKY
    }

    private fun disconnect() {
        isRunning = false
        Usqueandroid.stopTunnel()
        try {
            outputStream?.close()
            vpnInterface?.close()
        } catch (e: Exception) {}
        vpnInterface = null
        outputStream = null
        stopSelf()
    }

    override fun onDestroy() {
        disconnect()
        instance = null
        super.onDestroy()
    }

    override fun onRevoke() {
        disconnect()
        super.onRevoke()
    }
}
