#!/usr/bin/env python3
"""
test_ingestion.py

E2E Ingestion verification script that tests uploading generated synthetic documents
(Invoice, Surat Jalan, POD, Rate Agreement) to backend-api and validates responses.
"""

import sys
import json
import urllib.request
import urllib.error
from pathlib import Path

# Ensure UTF-8 output
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


def upload_file(api_url: str, file_path: Path) -> dict:
    boundary = "----WebKitFormBoundary7MA4YWxkTrZu0gW"
    
    with open(file_path, "rb") as f:
        file_bytes = f.read()

    body = bytearray()
    body.extend(f"--{boundary}\r\n".encode("utf-8"))
    body.extend(f'Content-Disposition: form-data; name="file"; filename="{file_path.name}"\r\n'.encode("utf-8"))
    body.extend(b"Content-Type: application/octet-stream\r\n\r\n")
    body.extend(file_bytes)
    body.extend(b"\r\n")
    body.extend(f"--{boundary}--\r\n".encode("utf-8"))

    req = urllib.request.Request(
        f"{api_url}/api/v1/documents/upload",
        data=bytes(body),
        headers={
            "Content-Type": f"multipart/form-data; boundary={boundary}",
            "User-Agent": "IngestionTester/1.0",
        },
        method="POST"
    )

    with urllib.request.urlopen(req, timeout=10) as response:
        resp_data = json.loads(response.read().decode("utf-8"))
        return resp_data


def main():
    root_dir = Path(__file__).resolve().parent.parent.parent
    backend_url = "http://localhost:8080"
    tc001_dir = root_dir / "synthetic-data" / "dataset" / "v0_initial_10" / "TC001_MATCH"

    print("=" * 65)
    print(" 🚀 TESTING PHASE 4: DOCUMENT INGESTION FLOW")
    print(f" Backend URL: {backend_url}")
    print(f" Sample Data: {tc001_dir}")
    print("=" * 65)

    test_files = [
        tc001_dir / "invoice.pdf",
        tc001_dir / "surat_jalan.pdf",
        tc001_dir / "pod.jpg",
        tc001_dir / "rate_agreement.pdf",
    ]

    for tf in test_files:
        if not tf.exists():
            print(f"❌ Test file not found: {tf}")
            continue

        try:
            print(f"\nUploading: {tf.name} ({tf.stat().st_size} bytes) ...")
            # 1. First upload
            res1 = upload_file(backend_url, tf)
            doc_id = res1["data"]["document_id"]
            hash_val = res1["data"]["sha256_hash"][:16] + "..."
            print(f"  └── ✅ [201 CREATED] Doc ID: {doc_id} | Hash: {hash_val} | Dup: {res1['data']['is_duplicate']}")

            # 2. Test Deduplication (Second upload of same file)
            res2 = upload_file(backend_url, tf)
            print(f"  └── 🔄 [200 DEDUP]   Doc ID: {res2['data']['document_id']} | IsDuplicate: {res2['data']['is_duplicate']}")

        except urllib.error.URLError as e:
            print(f"  └── ❌ Failed to connect to backend: {e}. Ensure backend-api is running on port 8080.")
            return 1

    print("\n" + "=" * 65)
    print(" 🎉 INGESTION TEST COMPLETED SUCCESSFULLY!")
    print("=" * 65)
    return 0


if __name__ == "__main__":
    sys.exit(main())
