# SonKoz Glide

Türkiye'deki internet servis sağlayıcılarının DPI (derin paket inceleme) tabanlı
engellerini aşmak için Windows masaüstü uygulaması. Discord, Roblox ve diğer
platformlara erişimi tek tuşla açar. VPN değildir: trafiğiniz hiçbir sunucuya
yönlendirilmez, sadece bilgisayarınızdan çıkan paketlerin biçimi değiştirilir.

> **English:** SonKoz Glide is a Windows desktop app (Go + Wails) that wraps
> [zapret](https://github.com/bol-van/zapret)'s `winws` to bypass DPI-based
> blocking by Turkish ISPs. It measures which desync strategy actually works on
> the user's line, verifies it with real TLS handshakes, and keeps monitoring it.
> Not a VPN — no traffic leaves through a third-party server.

## Nasıl çalışır

- **Paket düzenleme:** [zapret](https://github.com/bol-van/zapret)'in `winws`
  aracı, [WinDivert](https://github.com/basil00/WinDivert) sürücüsüyle sadece
  hedef alan adlarına giden TLS/QUIC el sıkışmalarını değiştirir.
- **Hatta ölçülen strateji:** Her bağlantıda bir strateji kataloğu
  ([`internal/engine/strategy.go`](internal/engine/strategy.go)) sırayla denenir;
  her aday gerçek Discord/Roblox sunucularına yapılan el sıkışmalarıyla
  doğrulanır, geçemeyen elenir. Hattınızda çalışan strateji öğrenilir ve bir
  sonraki açılışta ilk denenir.
- **Sürekli izleme:** Bağlantı açıkken güvenilirlik oranı ölçülür; operatör
  davranışını değiştirirse Glide yeni strateji arar. İnternet tamamen
  kesildiğinde gereksiz yeniden başlatma yapmaz.
- **Güvenli DNS (isteğe bağlı):** DNS zehirlemesine karşı Cloudflare/Google DoH
  uygulanır, kapatıldığında önceki ayarlar geri yüklenir.
- **Bağlantı Doktoru (isteğe bağlı):** TCP pencere ölçekleme ve MTU sorunlarını
  tespit edip düzeltir, kapatıldığında geri alır.

Kapsanan platformlar [`rules/rules.yaml`](rules/rules.yaml) dosyasında tanımlıdır:
Discord, Roblox, OpenAI, Telegram, YouTube ve Meta (Instagram, WhatsApp).

## Kullanım

1. [Releases](https://github.com/SonKoz-Game/SonKozGlide/releases) sayfasından
   `SonKozGlide_windows_amd64.exe` dosyasını indirin. Her release'te SHA-256
   özetleri `checksums.txt` içinde yer alır; uygulama içi güncelleme de bu
   dosyayla doğrulanır.
2. Çalıştırın (WinDivert sürücüsü için yönetici izni gerekir).
3. Güç düğmesine basın. Glide hattınıza uygun modu ölçer ve bağlantıyı açar.

Yeni sürüm çıktığında ana ekranda güncelleme düğmesi belirir. İndirme
ilerlemesi düğmede görünür; bitince **Yeniden başlat** ile yeni sürüm açılır ve
bağlantı açıksa kaldığı yerden devam eder.

Gereksinimler: Windows 10/11, 64 bit.

## Windows uyarısı ve güvenlik

İlk çalıştırmada Windows **"Windows kişisel bilgisayarınızı korudu"** uyarısı
gösterebilir. Bu uyarı Microsoft Defender SmartScreen'den gelir ve dosyanın
ücretli bir kod imzalama sertifikasıyla imzalanmadığını, ayrıca yeni bir dosya
olarak henüz yeterli indirme geçmişi olmadığını belirtir. Zararlı yazılım tespiti
değildir. Devam etmek için **Ek bilgi → Yine de çalıştır**'a tıklayın.
Uygulama içinden yapılan güncellemelerde bu uyarı tekrar çıkmaz.

Dosyaya körü körüne güvenmek zorunda değilsiniz:

- **Kaynak açık:** Uygulamanın tüm kodu bu depodadır.
- **GitHub derliyor:** Release dosyaları bir bilgisayarda elle değil,
  [release iş akışı](.github/workflows/release.yml) tarafından etiketlenmiş
  koddan derlenir.
- **Özet kontrolü:** Her release'teki `checksums.txt`, dosyaların SHA-256
  özetlerini içerir. Uygulama içi güncelleme indirdiği dosyayı bu özetle
  doğrular, uyuşmazsa kurmaz. Elle kontrol etmek için:

  ```powershell
  Get-FileHash .\SonKozGlide_windows_amd64.exe -Algorithm SHA256
  ```

- **Derleme kanıtı:** Release dosyaları GitHub tarafından imzalanmış bir
  derleme kanıtı (build provenance attestation) taşır. Dosyanın bu depodaki
  koddan, GitHub Actions'ta derlendiğini [GitHub CLI](https://cli.github.com/)
  ile doğrulayabilirsiniz:

  ```bash
  gh attestation verify SonKozGlide_windows_amd64.exe -R SonKoz-Game/SonKozGlide
  ```

**Antivirüs uyarıları:** Bazı antivirüs programları WinDivert sürücüsünü
"HackTool" veya "PUA" (istenmeyebilecek uygulama) olarak işaretleyebilir. Bu
kategori, ağ paketlerini değiştirebilen araçlar için kullanılır; WinDivert,
GoodbyeDPI ve zapret gibi açık kaynak araçların da kullandığı sürücüdür.

**Uygulamanın sisteminizde yaptıkları:** Bağlantı açıkken `winws.exe`
çalıştırılır. İsteğe bağlı olarak DNS sunucuları şifreli DNS'e (Cloudflare,
Google) ve TCP/MTU ayarları düzeltilmiş değerlere çevrilir. Bağlantı
kapatıldığında önceki ayarlar geri yüklenir. Uygulama telemetri veya kişisel
veri toplamaz; ayarlar ve kayıtlar yalnızca bilgisayarınızda tutulur.

Uygulamanın kendi açtığı ağ bağlantıları:

- Erişim kontrolü için Discord ve Roblox sunucularına TLS el sıkışmaları; veri
  gönderilmez.
- İnternetin açık olup olmadığını anlamak için Google, Microsoft ve Cloudflare'e
  TLS el sıkışmaları.
- Cloudflare (`1.1.1.1`) ve Google (`8.8.8.8`) üzerinden DNS sorguları ve MTU
  ölçümü için bu adreslere ping.
- GitHub üzerinden sürüm kontrolü ve güncelleme indirme.

## Kaldırma

**Tercihler → Glide'ı bilgisayardan kaldır** Glide'ın yaptığı her değişikliği
geri alır ve dosyalarını siler:

- Bağlantıyı ve Glide klasöründen çalışan her süreci (ör. sahipsiz kalmış
  `winws.exe`) durdurur; DNS, TCP ve MTU ayarlarını önceki değerlerine döndürür.
- Başlangıç görevini (Görev Zamanlayıcı → `SonKozGlide`) siler.
- WinDivert sürücüsünü durdurup kaydını kaldırır. Glide'ın sürücüsünü o anda
  başka bir uygulama kullanıyorsa önce o uygulama kapatılır. Sürücü başka bir
  uygulamaya aitse dokunulmaz.
- `C:\ProgramData\SonKozGlide` (bileşenler, kural listesi, öğrenilen modlar,
  kayıtlar), `%USERPROFILE%\.sonkoz` (ayarlar) ve `%APPDATA%\<exe adı>`
  (arayüz önbelleği) klasörlerini siler.
- Pencere kapandıktan sonra uygulama dosyasının kendisini siler.

Yeniden başlatma gerekmez: Glide'ın kendi açık tuttuğu dosyalar da pencere
kapanır kapanmaz silinir.

Uygulama dosyasını kaldırmadan sildiyseniz en kolay yol, Glide'ı yeniden indirip
bu düğmeyi kullanmaktır; DNS yedeği de böylece geri yüklenir.

## Geliştirme

Gerekenler: [Go 1.26+](https://go.dev/dl/), [Node.js 22+](https://nodejs.org/),
[Wails CLI v2.16](https://wails.io/docs/gettingstarted/installation):

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
```

Testler (frontend derlemesi gerekmez):

```bash
go test ./internal/...
```

`go vet ./...` ve kök paketin derlenmesi `frontend/dist` klasörüne ihtiyaç duyar,
önce frontend'i derleyin:

```bash
cd frontend
npm ci
npm run build
```

Sürüm derlemesi (sürüm numarası `wails.json` içindeki `info.productVersion`
alanında tutulur; `build.bat` onu günceller ve EXE'ye gömer):

```bat
build.bat 1.2.0
```

Ağ özellikleri (WinDivert, DNS, MTU) yönetici izni gerektirir; geliştirme
sırasında uygulamayı yönetici olarak başlatın.

### Sürüm yayınlamak

1. `build.bat 1.4.0` veya `scripts/version.ps1 -Set 1.4.0` ile `wails.json`
   içindeki sürümü güncelleyip commit edin.
2. Aynı sürümle etiket oluşturup gönderin:

   ```bash
   git tag v1.4.0 && git push origin v1.4.0
   ```

[Release iş akışı](.github/workflows/release.yml) etiketi `wails.json` ile
karşılaştırır, testleri çalıştırır, EXE'yi derler ve release'e
`SonKozGlide_windows_amd64.exe`, `checksums.txt` ve `licenses.zip` dosyalarını
ekler. Güncelleyici yalnızca `windows_amd64.exe` ile biten dosyaları tanır; dosya
adını değiştirmeyin.

### Proje yapısı

| Yol | İçerik |
|---|---|
| `main.go`, `app.go`, `uninstall.go` | Wails uygulaması, sistem tepsisi, otomatik başlatma, kaldırma, arayüz bağlamaları |
| `internal/engine` | winws yaşam döngüsü, strateji tarama ve doğrulama, sağlık izleme, DNS ve ağ ayarları, kurulum temizliği |
| `internal/router` | `rules.yaml` dosyasını winws hostlist'ine derler |
| `internal/settings` | Kullanıcı tercihleri (`%USERPROFILE%\.sonkoz\settings.json`) |
| `internal/updater` | GitHub Releases üzerinden sürüm kontrolü, ilerlemeli güncelleme |
| `internal/instance` | Tek kopya kilidi; yeniden başlatmada eski kopyanın kapanmasını bekler |
| `frontend/` | Arayüz (Vite, saf JavaScript) |
| `resources/` | Gömülü üçüncü taraf ikililer ve tepsi simgeleri |
| `rules/rules.yaml` | Platform ve alan adı listesi |

### Yeni bir platform eklemek

[`rules/rules.yaml`](rules/rules.yaml) içine platformu ve alan adlarını ekleyin.
Paylaşılan altyapı alan adlarını (ör. `auth0.com`, `gvt1.com`, ödeme
sağlayıcıları) eklemeyin: hostlist son ekle eşleştiği için o altyapıyı kullanan
tüm sitelerin trafiği değiştirilir. `internal/router/ruleset_test.go` bu
durumu denetler.

## Lisans

SonKoz Glide'ın kaynak kodu [MIT Lisansı](LICENSE) ile yayınlanmıştır.
Gömülü üçüncü taraf bileşenler (zapret/winws — MIT, WinDivert — LGPL-3.0/GPL-2.0,
Cygwin — LGPL-3.0) kendi lisanslarına tabidir; ayrıntılar
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) dosyasındadır.

Bu yazılım "olduğu gibi" sunulur. Kullanımın bulunduğunuz ülkenin mevzuatına
uygunluğundan kullanıcı sorumludur.
