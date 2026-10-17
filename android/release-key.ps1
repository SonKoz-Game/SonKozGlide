$ErrorActionPreference = "Stop"

$repo = "SonKoz-Game/SonKozGlide"
$alias = "sonkozglide"
$dir = Join-Path $env:USERPROFILE ".sonkoz"
$keystore = Join-Path $dir "glide-release.jks"

$keytool = "keytool"
if ($env:JAVA_HOME) { $keytool = Join-Path $env:JAVA_HOME "bin\keytool.exe" }

if (Test-Path $keystore) {
    Write-Host "Anahtar zaten var: $keystore"
    Write-Host "Yalnızca GitHub secret'ları yeniden yazılacak."
} else {
    New-Item -ItemType Directory -Force $dir | Out-Null
}

$secure = Read-Host "Anahtar parolası (en az 8 karakter)" -AsSecureString
$plain = [Runtime.InteropServices.Marshal]::PtrToStringBSTR([Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure))
if ($plain.Length -lt 8) { throw "Parola en az 8 karakter olmalı." }

$env:SK_GEN_PASS = $plain
try {
    if (-not (Test-Path $keystore)) {
        & $keytool -genkeypair -keystore $keystore -storetype PKCS12 -alias $alias `
            -keyalg RSA -keysize 4096 -validity 36500 `
            -dname "CN=SonKoz Glide, O=SonKoz, C=TR" `
            -storepass:env SK_GEN_PASS -keypass:env SK_GEN_PASS
        if ($LASTEXITCODE) { throw "keytool başarısız oldu." }
    } else {
        & $keytool -list -keystore $keystore -storepass:env SK_GEN_PASS -alias $alias | Out-Null
        if ($LASTEXITCODE) { throw "Parola bu anahtarla eşleşmiyor." }
    }

    $b64 = [Convert]::ToBase64String([IO.File]::ReadAllBytes($keystore))
    $b64 | gh secret set SK_KEYSTORE_B64 --repo $repo
    $plain | gh secret set SK_KEYSTORE_PASSWORD --repo $repo
    $plain | gh secret set SK_KEY_PASSWORD --repo $repo
    $alias | gh secret set SK_KEY_ALIAS --repo $repo
} finally {
    Remove-Item Env:SK_GEN_PASS -ErrorAction SilentlyContinue
}

Write-Host ""
Write-Host "Tamam. Anahtar: $keystore"
Write-Host "Bu dosyayı ve parolayı güvenli bir yere yedekle. Kaybolursa Android sürümü bir daha güncellenemez."
