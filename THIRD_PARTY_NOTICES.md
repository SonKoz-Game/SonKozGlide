# Third-party notices

SonKoz Glide's own source code is released under the [MIT License](LICENSE).
The application bundles or links the third-party components listed below. Each
remains under its own license; the full license texts are in [`licenses/`](licenses).

## Bundled binaries (`resources/`)

These files are embedded into `SonKozGlide.exe` and extracted to
`%ProgramData%\SonKozGlide` at runtime. They are distributed unmodified.

| File | Component | License | Upstream |
|---|---|---|---|
| `winws.exe` | zapret (winws, Windows build) — Copyright (c) 2016-2024 bol-van | MIT — [`licenses/zapret-MIT.txt`](licenses/zapret-MIT.txt) | https://github.com/bol-van/zapret |
| `WinDivert.dll`, `WinDivert64.sys` | WinDivert 2.2.2 — Copyright © Basil 2011-2022 | LGPL-3.0 or GPL-2.0 — [`licenses/WinDivert-LICENSE.txt`](licenses/WinDivert-LICENSE.txt) | https://github.com/basil00/WinDivert |
| `cygwin1.dll` | Cygwin 3.4.10 — Copyright © Cygwin Authors 1996-2023 | LGPL-3.0-or-later — [`licenses/LGPL-3.0.txt`](licenses/LGPL-3.0.txt), [`licenses/GPL-3.0.txt`](licenses/GPL-3.0.txt) | https://cygwin.com/licensing.html |

Corresponding source for the LGPL components is available from their upstream
projects: WinDivert 2.2.2 at https://github.com/basil00/WinDivert/tree/v2.2.2 and
Cygwin 3.4.10 at https://github.com/cygwin/cygwin/tree/cygwin-3.4.10.

SHA-256 of the bundled files:

```
2da71e80878dc270ac83f5893ecbb841f9752a57f1da8ff9325636b4346bc632  winws.exe
c1e060ee19444a259b2162f8af0f3fe8c4428a1c6f694dce20de194ac8d7d9a2  WinDivert.dll
8da085332782708d8767bcace5327a6ec7283c17cfb85e40b03cd2323a90ddc2  WinDivert64.sys
103104a52e5293ce418944725df19e2bf81ad9269b9a120d71d39028e821499b  cygwin1.dll
```

## Frontend assets (installed with npm)

| Package | License |
|---|---|
| `@fontsource/figtree` — Figtree, Copyright 2022 The Figtree Project Authors | SIL Open Font License 1.1 |
| `@fortawesome/fontawesome-free` 6 — Font Awesome Free by Fonticons, Inc. | Icons: CC BY 4.0, fonts: SIL OFL 1.1, code: MIT |
| `vite` (build only) | MIT |

## Go modules compiled into the executable

| Module | License |
|---|---|
| `github.com/wailsapp/wails/v2` | MIT |
| `github.com/getlantern/systray` | Apache-2.0 |
| `github.com/creativeprojects/go-selfupdate` | MIT |
| `golang.org/x/sys` | BSD-3-Clause |
| `gopkg.in/yaml.v3` | MIT and Apache-2.0 |

Indirect dependencies are listed in [`go.mod`](go.mod); their license files ship
with each module in the Go module cache.
