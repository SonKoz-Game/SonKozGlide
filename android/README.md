# SonKoz Glide — Android

Root gerektirmeden, yerel bir `VpnService` tüneli ile Türk ISS'lerinde Discord ve
Roblox engellerini aşan Android istemcisi.

## Nasıl çalışır

- `android/core` — gomobile ile AAR'a derlenen Go çekirdeği.
  - Şifreli DNS (DoH, RFC 8484) ile isim çözümü; düz UDP DNS kullanılmaz.
  - Yalnızca Discord/Roblox alan adlarına sahte IP verilir; bu IP'ler tünele
    yönlendirilir, diğer tüm trafik dokunulmadan geçer.
  - Sahte IP'ye gelen TCP bağlantıları gVisor ağ yığınında karşılanır, gerçek
    sunucuya açılır ve ilk TLS ClientHello düşük TTL'li sahte paket + SNI bölme
    ile gönderilir.
- `android/app` — Kotlin + Jetpack Compose arayüz ve `VpnService`. Tek düğme
  bağlantıyı açar/kapatır.

## Derleme

```bash
# 1) Go çekirdeğini AAR'a derle
cd android
./build-aar.sh

# 2) APK üret
./gradlew assembleDebug
# çıktı: app/build/outputs/apk/debug/app-debug.apk
```

Gereksinimler: Android SDK (platform 36, build-tools 36.1.0), NDK (28.x),
JDK 17, Go 1.27+ ve `gomobile`/`gobind` (`go install golang.org/x/mobile/cmd/...@latest`).
SDK yolu `local.properties` içindeki `sdk.dir` ile verilir.

## Alan adları

Çözüm `android/core/glide/rules.go` içindeki listeden gelir ve masaüstü
`rules/rules.yaml` ile aynı Discord/Roblox alan adlarını kapsar.
