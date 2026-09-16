"""Rebuild the embedded Noto subtitle fonts from pinned upstream revisions."""

import hashlib
import io
import json
from pathlib import Path
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
NOTO = "https://raw.githubusercontent.com/notofonts/noto-fonts/ffebf8c1ee449e544955a7e813c54f9b73848eac/"
CJK = "https://raw.githubusercontent.com/notofonts/noto-cjk/f8d157532fbfaeda587e826d4cd5b21a49186f7c/"
# Script tags are ISO 15924. The first entry is the default face.
FAMILIES = {
    "NotoSans": ["Latn", "Grek", "Cyrl", "Zyyy", "Zinh"],
    "NotoSansArabic": ["Arab"],
    "NotoSansHebrew": ["Hebr"],
    "NotoSansDevanagari": ["Deva"],
    "NotoSansBengali": ["Beng"],
    "NotoSansTamil": ["Taml"],
    "NotoSansTelugu": ["Telu"],
    "NotoSansMalayalam": ["Mlym"],
    "NotoSansKannada": ["Knda"],
    "NotoSansGujarati": ["Gujr"],
    "NotoSansGurmukhi": ["Guru"],
    "NotoSansSinhala": ["Sinh"],
    "NotoSansThai": ["Thai"],
    "NotoSansLao": ["Laoo"],
    "NotoSansKhmer": ["Khmr"],
    "NotoSansMyanmar": ["Mymr"],
    "NotoSansGeorgian": ["Geor"],
    "NotoSansArmenian": ["Armn"],
    "NotoSansEthiopic": ["Ethi"],
    "NotoSerifTibetan": ["Tibt"],
    "NotoSansSymbols": [],
    "NotoSansSymbols2": [],
}


def fetch(url):
    with urllib.request.urlopen(url, timeout=120) as response:
        return response.read()


def build():
    """Store original fonts, their provenance, and licenses in a deterministic ZIP."""
    entries = {}
    manifest = []
    sources = [(f"{family}-Regular.ttf", scripts, NOTO + f"hinted/ttf/{family}/{family}-Regular.ttf") for family, scripts in FAMILIES.items()]
    sources.append(("NotoSansCJKjp-Regular.otf", ["Hani", "Hira", "Kana", "Hang", "Bopo"], CJK + "Sans/OTF/Japanese/NotoSansCJKjp-Regular.otf"))
    for name, scripts, url in sources:
        data = fetch(url)
        entries[name] = data
        manifest.append(dict(file=name, scripts=scripts, url=url, sha256=hashlib.sha256(data).hexdigest()))
        print(name, len(data), flush=True)
    entries["manifest.json"] = (json.dumps(manifest, indent=2) + "\n").encode()
    entries["LICENSE-Noto.txt"] = fetch(NOTO + "LICENSE")
    entries["LICENSE-CJK.txt"] = fetch(CJK + "Sans/LICENSE")
    # Keep fonts uncompressed in the executable. Reading embedded bytes avoids
    # a multi-second first-caption inflate on MiSTer. Release ZIPs compress them.
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_STORED) as archive:
        for name, data in sorted(entries.items()):
            info = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_STORED
            info.external_attr = 0o100644 << 16
            archive.writestr(info, data)
    destination = ROOT / "internal/caption/fonts.zip"
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(output.getvalue())
    (ROOT / "docs/licenses/noto.txt").write_bytes(entries["LICENSE-Noto.txt"] + b"\n\nNoto CJK:\n\n" + entries["LICENSE-CJK.txt"])
    print(destination, len(output.getvalue()), flush=True)


if __name__ == "__main__":
    build()
