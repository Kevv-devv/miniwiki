#!/usr/bin/env python3
"""Package platform binaries into portable, reproducible release archives.

No third-party Python dependencies are required.
"""

import argparse
import gzip
import hashlib
from pathlib import Path
import re
import tarfile
import zipfile

PLATFORMS = (
    "linux-amd64", "linux-arm64", "windows-amd64",
    "darwin-amd64", "darwin-arm64",
    "freebsd-amd64", "freebsd-arm64",
    "openbsd-amd64", "openbsd-arm64", "netbsd-amd64",
)
DOCS = (
    "LICENSE", "NOTICE", "README.md", "config.example.toml",
    "docs/INSTALL_FROM_RELEASE.md", "docs/DEPLOYMENT.md",
    "docs/RELEASING.md", "docs/SCREENSHOTS.md", "docs/README_LEGACY.md",
)
VERSION_PATTERN = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?\Z")


def create_packages(version: str, input_dir: Path, output_dir: Path, repo_root: Path):
    if not VERSION_PATTERN.fullmatch(version):
        raise ValueError(f"Invalid release version: {version}")

    files = []
    for platform in PLATFORMS:
        suffix = ".exe" if platform.startswith("windows-") else ""
        binary = input_dir / f"miniwiki-{platform}{suffix}"
        if not binary.is_file() or binary.stat().st_size == 0:
            raise FileNotFoundError(f"Missing or empty executable: {binary}")
        files.append((platform, binary, f"miniwiki{suffix}"))

    for doc in DOCS:
        if not (repo_root / doc).is_file():
            raise FileNotFoundError(f"Missing release document: {doc}")

    # Include real, reviewed screenshots only if they were committed in the source.
    screenshots = sorted((repo_root / "docs/images").glob("*.png")) if (repo_root / "docs/images").is_dir() else []

    if output_dir.exists() and any(output_dir.iterdir()):
        raise FileExistsError(f"Refusing nonempty output directory: {output_dir}")
    output_dir.mkdir(parents=True, exist_ok=True)

    created = []
    for platform, binary, executable_name in files:
        stem = f"miniwiki-{version}-{platform}"
        members = [(binary, executable_name, True)]
        members.extend((repo_root / doc, doc, False) for doc in DOCS)
        members.extend((img, img.relative_to(repo_root).as_posix(), False) for img in screenshots)

        if platform.startswith("windows-"):
            target = output_dir / f"{stem}.zip"
            with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED,
                                 compresslevel=9) as archive:
                for source, name, executable in members:
                    entry = zipfile.ZipInfo(f"{stem}/{name}", (1980, 1, 1, 0, 0, 0))
                    entry.create_system = 3
                    entry.external_attr = (0o100755 if executable else 0o100644) << 16
                    entry.compress_type = zipfile.ZIP_DEFLATED
                    archive.writestr(entry, source.read_bytes())
        else:
            target = output_dir / f"{stem}.tar.gz"
            with target.open("wb") as stream:
                with gzip.GzipFile(fileobj=stream, mode="wb", filename="", mtime=0) as gz:
                    with tarfile.open(fileobj=gz, mode="w") as archive:
                        for source, name, executable in members:
                            info = tarfile.TarInfo(f"{stem}/{name}")
                            info.size = source.stat().st_size
                            info.mode = 0o755 if executable else 0o644
                            info.uid = info.gid = 0
                            info.uname = info.gname = ""
                            info.mtime = 0
                            with source.open("rb") as data:
                                archive.addfile(info, data)
        print(f"Created: {target.name}")
        created.append(target)

    checksum_file = output_dir / "SHA256SUMS"
    with checksum_file.open("w", encoding="utf-8", newline="\n") as listing:
        for path in sorted(created):
            digest = hashlib.sha256(path.read_bytes()).hexdigest()
            listing.write(f"{digest}  {path.name}\n")
    print(f"Done: {len(created)} archives + SHA256SUMS")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--input-dir", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    args = parser.parse_args()
    repo_root = Path(__file__).resolve().parent.parent
    create_packages(args.version, args.input_dir, args.output_dir, repo_root)


if __name__ == "__main__":
    main()
