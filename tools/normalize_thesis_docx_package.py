"""Repackage the DOCX through python-docx while retaining all existing parts."""

from __future__ import annotations

import os
import tempfile
from pathlib import Path

from docx import Document


DOCX = Path(__file__).resolve().parents[1] / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"


def main() -> None:
    if not DOCX.is_file():
        raise FileNotFoundError(f"Final thesis file not found: {DOCX}")
    fd, temporary_name = tempfile.mkstemp(prefix="thesis-package-normalize-", suffix=".docx", dir=DOCX.parent)
    os.close(fd)
    temporary = Path(temporary_name)
    try:
        document = Document(DOCX)
        document.save(temporary)
        os.replace(temporary, DOCX)
    finally:
        if temporary.exists():
            temporary.unlink()
    print(f"Repackaged {DOCX.name} with Word-compatible XML.")


if __name__ == "__main__":
    main()
