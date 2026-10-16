# The JavaScript bridge is called by name from the web UI.
-keepclassmembers class io.github.geckoterm.android.Bridge {
    @android.webkit.JavascriptInterface <methods>;
}
