"""Remove the failed manual drawings and restore stable ASCII image markers."""

from __future__ import annotations

import os
import tempfile
import zipfile
from pathlib import Path

from lxml import etree


ROOT = Path(__file__).resolve().parents[1]
DOCX = ROOT / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"
W_NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
PIC_NS = "http://schemas.openxmlformats.org/drawingml/2006/picture"
W = f"{{{W_NS}}}"
NSMAP = {"pic": PIC_NS, "w": W_NS}

MARKERS = {
    "figure-2-1-workflow.png": "[[FIGURE_2_1]]",
    "figure-3-1-four-ledgers.png": "[[FIGURE_3_1]]",
    "figure-3-3-tax-boundary.png": "[[FIGURE_3_2]]",
    "figure-3-2-sequence.png": "[[FIGURE_3_3]]",
    "figure-4-1-demo-flow.png": "[[FIGURE_4_1]]",
}


def main() -> None:
    fd, temporary_name = tempfile.mkstemp(prefix="thesis-figure-slot-restore-", suffix=".docx", dir=DOCX.parent)
    os.close(fd)
    temporary = Path(temporary_name)
    restored = 0
    try:
        with zipfile.ZipFile(DOCX, "r") as source:
            document = etree.fromstring(source.read("word/document.xml"))
            rels = etree.fromstring(source.read("word/_rels/document.xml.rels"))

            for picture in document.xpath("//pic:pic", namespaces=NSMAP):
                names = picture.xpath("./pic:nvPicPr/pic:cNvPr/@name", namespaces=NSMAP)
                if not names or names[0] not in MARKERS:
                    continue
                drawing = picture.xpath("ancestor::w:drawing", namespaces=NSMAP)[0]
                run = drawing.getparent()
                paragraph = run.getparent()
                paragraph.remove(run)
                replacement_run = etree.SubElement(paragraph, W + "r")
                replacement_text = etree.SubElement(replacement_run, W + "t")
                replacement_text.text = MARKERS[names[0]]
                restored += 1

            if restored != len(MARKERS):
                raise RuntimeError(f"Restored {restored} image slots; expected {len(MARKERS)}")

            for relationship in list(rels):
                if relationship.get("Target", "").startswith("media/figure-"):
                    rels.remove(relationship)

            with zipfile.ZipFile(temporary, "w", zipfile.ZIP_DEFLATED) as destination:
                for item in source.infolist():
                    if item.filename.startswith("word/media/figure-"):
                        continue
                    if item.filename == "word/document.xml":
                        destination.writestr(item, etree.tostring(document, xml_declaration=True, encoding="UTF-8", standalone=True))
                    elif item.filename == "word/_rels/document.xml.rels":
                        destination.writestr(item, etree.tostring(rels, xml_declaration=True, encoding="UTF-8", standalone=True))
                    else:
                        destination.writestr(item, source.read(item.filename))
        os.replace(temporary, DOCX)
    finally:
        if temporary.exists():
            temporary.unlink()
    print(f"Restored {restored} stable figure markers.")


if __name__ == "__main__":
    main()
