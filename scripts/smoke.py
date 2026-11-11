#!/usr/bin/env python3
# Native creation requires a real first message; opt in to inference explicitly.
import sys,subprocess
from pathlib import Path
if '--inference' not in sys.argv:
    raise SystemExit('Use --inference: native sessions are created by sending a first message.')
raise SystemExit(subprocess.call([sys.executable,str(Path(__file__).with_name('native-roundtrip.py'))]))
