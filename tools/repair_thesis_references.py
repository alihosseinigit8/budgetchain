"""Repair the verified NISTIR 8460 entry and remove the non-public source [10]."""

from __future__ import annotations

import os
import tempfile
import zipfile
from pathlib import Path

from lxml import etree


DOCX = Path(__file__).resolve().parents[1] / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"
NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
W = f"{{{NS}}}"
NSMAP = {"w": NS}

OLD_REFERENCE_7 = "[۷] D. A. Basin et al., State Machine Replication and Consensus with Byzantine Adversaries, NISTIR 8460, National Institute of Standards and Technology, 2023."
NEW_REFERENCE_7 = "[۷] M. Davidson, State Machine Replication and Consensus with Byzantine Adversaries, NISTIR 8460 (Initial Public Draft), National Institute of Standards and Technology, 2023. doi: 10.6028/NIST.IR.8460.ipd."
REFERENCE_10 = "[۱۰] BudgetChain، مخزن کد منبع پروژه و آزمون‌های خودکار، نسخه‌ی پروژه‌ی کارشناسی، ۱۴۰۵."
CITATION_10 = "[۱۰]"


def text_of(paragraph: etree._Element) -> str:
    return "".join(paragraph.xpath(".//w:t/text()", namespaces=NSMAP)).strip()


def replace_paragraph_text(paragraph: etree._Element, new_text: str) -> None:
    """Replace visible text while retaining paragraph-level formatting."""
    runs = paragraph.xpath("./w:r", namespaces=NSMAP)
    if not runs:
        run = etree.SubElement(paragraph, f"{W}r")
        runs = [run]
    first = runs[0]
    for child in list(first):
        if child.tag != f"{W}rPr":
            first.remove(child)
    text = etree.SubElement(first, f"{W}t")
    text.text = new_text
    if new_text[:1].isspace() or new_text[-1:].isspace():
        text.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")
    for run in runs[1:]:
        paragraph.remove(run)


def main() -> None:
    if not DOCX.is_file():
        raise FileNotFoundError(f"Final thesis file not found: {DOCX}")

    fd, temporary_name = tempfile.mkstemp(prefix="thesis-reference-fix-", suffix=".docx", dir=DOCX.parent)
    os.close(fd)
    temporary = Path(temporary_name)
    fixed_7 = 0
    removed_10 = 0
    cited_10 = 0

    try:
        with zipfile.ZipFile(DOCX, "r") as source:
            document = etree.fromstring(source.read("word/document.xml"))
            for paragraph in document.xpath("//w:body/w:p", namespaces=NSMAP):
                content = text_of(paragraph)
                if content == OLD_REFERENCE_7:
                    replace_paragraph_text(paragraph, NEW_REFERENCE_7)
                    fixed_7 += 1
                elif content == REFERENCE_10:
                    paragraph.getparent().remove(paragraph)
                    removed_10 += 1
                elif CITATION_10 in content:
                    # No in-text [10] occurs in the current document.  This
                    # guard removes one if a future file contains it, rather
                    # than replacing it with a fabricated source.
                    replace_paragraph_text(paragraph, content.replace(CITATION_10, ""))
                    cited_10 += 1

            with zipfile.ZipFile(temporary, "w", zipfile.ZIP_DEFLATED) as destination:
                for item in source.infolist():
                    if item.filename == "word/document.xml":
                        destination.writestr(
                            item,
                            etree.tostring(document, xml_declaration=True, encoding="UTF-8", standalone=True),
                        )
                    else:
                        destination.writestr(item, source.read(item.filename))

        os.replace(temporary, DOCX)
    finally:
        if temporary.exists():
            temporary.unlink()

    if fixed_7 != 1 or removed_10 != 1:
        raise RuntimeError(f"Expected one correction and one removal; got [7]={fixed_7}, [10]={removed_10}.")
    print(f"Corrected [7]; removed [10] and {cited_10} in-text [10] citation(s).")


if __name__ == "__main__":
    main()
