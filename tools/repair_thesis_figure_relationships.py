"""Repair conflicting DOCX relationship IDs created while embedding figures."""

from __future__ import annotations

import os
import re
import tempfile
import zipfile
from pathlib import Path

from lxml import etree


ROOT = Path(__file__).resolve().parents[1]
DOCX = ROOT / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"
R_NS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
A_NS = "http://schemas.openxmlformats.org/drawingml/2006/main"
PIC_NS = "http://schemas.openxmlformats.org/drawingml/2006/picture"
REL_NS = "http://schemas.openxmlformats.org/package/2006/relationships"


def qn(namespace: str, name: str) -> str:
    return f"{{{namespace}}}{name}"


def next_id(relationships: list[etree._Element]) -> int:
    numeric = []
    for relationship in relationships:
        match = re.fullmatch(r"rId(\d+)", relationship.get("Id", ""))
        if match:
            numeric.append(int(match.group(1)))
    return max(numeric, default=0) + 1


def main() -> None:
    if not DOCX.is_file():
        raise FileNotFoundError(f"Final thesis file not found: {DOCX}")

    fd, temporary_name = tempfile.mkstemp(prefix="thesis-relationship-repair-", suffix=".docx", dir=DOCX.parent)
    os.close(fd)
    temporary = Path(temporary_name)
    try:
        with zipfile.ZipFile(DOCX, "r") as source:
            document = etree.fromstring(source.read("word/document.xml"))
            relationships_root = etree.fromstring(source.read("word/_rels/document.xml.rels"))
            relationships = list(relationships_root)
            figure_relationships = [
                relationship
                for relationship in relationships
                if relationship.get("Type") == "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
                and relationship.get("Target", "").startswith("media/figure-")
            ]
            if len(figure_relationships) != 5:
                raise RuntimeError(f"Expected 5 figure image relationships; found {len(figure_relationships)}")

            number = next_id(relationships)
            target_to_id: dict[str, str] = {}
            for relationship in figure_relationships:
                identifier = f"rId{number}"
                number += 1
                relationship.set("Id", identifier)
                target_to_id[relationship.get("Target")] = identifier

            repaired = 0
            for picture in document.xpath("//pic:pic", namespaces={"pic": PIC_NS}):
                names = picture.xpath("./pic:nvPicPr/pic:cNvPr/@name", namespaces={"pic": PIC_NS})
                if not names:
                    continue
                target = f"media/{names[0]}"
                identifier = target_to_id.get(target)
                if identifier is None:
                    continue
                blips = picture.xpath(".//a:blip", namespaces={"a": A_NS})
                if len(blips) != 1:
                    raise RuntimeError(f"Expected one image blip for {target}")
                blips[0].set(qn(R_NS, "embed"), identifier)
                repaired += 1

            if repaired != 5:
                raise RuntimeError(f"Repaired {repaired} figure drawing(s), expected 5")

            # Reject duplicate relationship IDs before writing the repaired DOCX.
            identifiers = [relationship.get("Id") for relationship in relationships_root]
            if len(identifiers) != len(set(identifiers)):
                raise RuntimeError("Relationship IDs are still not unique")

            with zipfile.ZipFile(temporary, "w", zipfile.ZIP_DEFLATED) as destination:
                for item in source.infolist():
                    if item.filename == "word/document.xml":
                        destination.writestr(item, etree.tostring(document, xml_declaration=True, encoding="UTF-8", standalone=True))
                    elif item.filename == "word/_rels/document.xml.rels":
                        destination.writestr(item, etree.tostring(relationships_root, xml_declaration=True, encoding="UTF-8", standalone=True))
                    else:
                        destination.writestr(item, source.read(item.filename))

        os.replace(temporary, DOCX)
    finally:
        if temporary.exists():
            temporary.unlink()

    print("Repaired 5 image relationship IDs.")


if __name__ == "__main__":
    main()
