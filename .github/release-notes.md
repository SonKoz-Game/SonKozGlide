### Kurulum

`SonKozGlide_windows_amd64.exe` dosyasını indirip çalıştırın (Windows 10/11, yönetici izni gerekir).

Windows ilk açılışta **"Windows kişisel bilgisayarınızı korudu"** uyarısı gösterebilir. Bunun nedeni dosyanın ücretli bir kod imzalama sertifikasıyla imzalanmamış olması ve yeni bir sürüm olarak henüz yeterince indirilmemesidir; dosyanın zararlı olduğu anlamına gelmez. Devam etmek için **Ek bilgi → Yine de çalıştır**. Uygulama içinden yapılan güncellemelerde bu uyarı tekrar çıkmaz.

### Bu dosyanın bu koddan derlendiğini doğrulayın

Bu release, GitHub Actions tarafından etiketlenmiş açık kaynak koddan derlendi ve GitHub tarafından imzalanmış bir derleme kanıtı (build provenance attestation) taşır.

- SHA-256 özeti `checksums.txt` ile aynı olmalıdır (PowerShell):
  ```powershell
  Get-FileHash .\SonKozGlide_windows_amd64.exe -Algorithm SHA256
  ```
- Derleme kanıtını doğrulamak için ([GitHub CLI](https://cli.github.com/)):
  ```bash
  gh attestation verify SonKozGlide_windows_amd64.exe -R SonKoz-Game/SonKozGlide
  ```

Ayrıntılar: [Güvenli mi?](https://github.com/SonKoz-Game/SonKozGlide#windows-uyar%C4%B1s%C4%B1-ve-g%C3%BCvenlik)
