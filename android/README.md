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

# 2) APK üret (mimariye göre ayrı, küçültülmüş dosyalar)
./gradlew assembleRelease
# çıktı: app/build/outputs/apk/release/app-arm64-v8a-release.apk (~4 MB)
```

`SK_KEYSTORE` ortam değişkeni yoksa yerel release derlemesi debug anahtarıyla
imzalanır; bu dosyalar yalnızca deneme içindir.

## Sürüm yayınlama

Sürüm anahtarı bir kez `android/release-key.ps1` ile oluşturulur ve GitHub
secret'larına yazılır (`SK_KEYSTORE_B64`, `SK_KEYSTORE_PASSWORD`, `SK_KEY_ALIAS`,
`SK_KEY_PASSWORD`). Anahtar `%USERPROFILE%\.sonkoz\glide-release.jks` dosyasında
durur; yedeklenmeli, kaybolursa Android sürümü güncellenemez.

`v*` etiketi atıldığında `release.yml` Android APK'larını da derler ve sürüme
`SonKozGlide_android_arm64.apk` ile `SonKozGlide_android_armv7.apk` olarak ekler.

Gereksinimler: Android SDK (platform 36, build-tools 36.1.0), NDK (28.x),
JDK 17, Go 1.27+ ve `gomobile`/`gobind` (`go install golang.org/x/mobile/cmd/...@latest`).
SDK yolu `local.properties` içindeki `sdk.dir` ile verilir.

## Alan adları

Çözüm `android/core/glide/rules.go` içindeki listeden gelir ve masaüstü
`rules/rules.yaml` ile aynı Discord/Roblox alan adlarını kapsar.
