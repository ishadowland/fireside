# ProGuard rules for Fireside. Sprint 0 keeps it empty (debug-signed
# debug builds only). Sprint 1+ will add rules when release signing and
# R8 minification are wired in.

-keep class com.firesidechat.app.** { *; }