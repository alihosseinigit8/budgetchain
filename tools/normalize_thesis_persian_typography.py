"""Remove visually problematic Arabic combining hamza characters from the thesis.

The document uses the Persian spelling «ه‌ی» (heh + ZWNJ + Persian yeh) rather
than «هٔ» (heh + combining hamza above).  The latter is highlighted and can
render poorly in the selected Word font.  Only XML text is normalised; all
formatting, footnote references, relationships, images, and tables remain
unchanged.
"""

from __future__ import annotations

import os
import tempfile
import zipfile
from pathlib import Path


DOCX = Path(__file__).resolve().parents[1] / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"
SOURCE = "\u0647\u0654"  # ه + combining hamza above
REPLACEMENT = "\u0647\u200c\u06cc"  # ه + ZWNJ + Persian ی


def main() -> None:
    if not DOCX.is_file():
        raise FileNotFoundError(f"Final thesis file not found: {DOCX}")

    fd, temporary_name = tempfile.mkstemp(prefix="thesis-typography-fix-", suffix=".docx", dir=DOCX.parent)
    os.close(fd)
    temporary = Path(temporary_name)
    replacements = 0
    try:
        with zipfile.ZipFile(DOCX, "r") as source:
            with zipfile.ZipFile(temporary, "w", zipfile.ZIP_DEFLATED) as destination:
                for item in source.infolist():
                    data = source.read(item.filename)
                    if item.filename.endswith(".xml"):
                        content = data.decode("utf-8")
                        matches = content.count(SOURCE)
                        if matches:
                            content = content.replace(SOURCE, REPLACEMENT)
                            data = content.encode("utf-8")
                            replacements += matches
                    destination.writestr(item, data)

        # Close the source DOCX before replacing it: Windows otherwise keeps
        # the input archive locked.
        os.replace(temporary, DOCX)
    finally:
        if temporary.exists():
            temporary.unlink()

    print(f"Replaced {replacements} combining-hamza characters in {DOCX.name}.")


if __name__ == "__main__":
    main()
