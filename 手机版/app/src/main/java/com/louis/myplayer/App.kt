package com.louis.myplayer

import android.app.Application

class App : Application() {
    override fun onCreate() {
        super.onCreate()
        Library.init(this)
    }
}
