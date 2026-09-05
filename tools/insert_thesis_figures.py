"""Embed the five approved thesis diagrams into their existing figure slots."""

from __future__ import annotations

import os
import struct
import tempfile
import zipfile
from pathlib import Path

from lxml import etree


ROOT = Path(__file__).resolve().parents[1]
DOCX = ROOT / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"
FIGURES = ROOT / "assets" / "thesis_figures"

W_NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
R_NS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
WP_NS = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
A_NS = "http://schemas.openxmlformats.org/drawingml/2006/main"
PIC_NS = "http://schemas.openxmlformats.org/drawingml/2006/picture"
REL_NS = "http://schemas.openxmlformats.org/package/2006/relationships"
CT_NS = "http://schemas.openxmlformats.org/package/2006/content-types"
NSMAP = {"w": W_NS, "wp": WP_NS, "r": R_NS}
W = f"{{{W_NS}}}"

# Image file, DOCX media file, and accessibility text.  The image slots are
# matched by their existing document order, which avoids fragile Unicode text
# matching against Persian placeholder prose.
SLOTS = (
    (
        "figure-2-1-workflow.png",
        "figure-2-1-workflow.png",
        "Permissioned budget-allocation workflow",
    ),
    (
        "figure-3-1-four-ledgers.png",
        "figure-3-1-four-ledgers.png",
        "Four-ledger BudgetChain architecture",
    ),
    (
        "figure-3-3-tax-boundary.png",
        "figure-3-3-tax-boundary.png",
        "Tax data boundary and minimum disclosure",
    ),
    (
        "figure-3-2-sequence.png",
        "figure-3-2-sequence.png",
        "Signed request to budget release sequence",
    ),
    (
        "figure-4-1-demo-flow.png",
        "figure-4-1-demo-flow.png",
        "Proof-of-concept execution status flow",
    ),
)


def qn(namespace: str, name: str) -> str:
    return f"{{{namespace}}}{name}"


def text_of(paragraph: etree._Element) -> str:
    return "".join(paragraph.xpath(".//w:t/text()", namespaces=NSMAP)).strip()


def png_dimensions(data: bytes) -> tuple[int, int]:
    if data[:8] != b"\x89PNG\r\n\x1a\n" or data[12:16] != b"IHDR":
        raise ValueError("Expected a PNG with an IHDR chunk")
    return struct.unpack(">II", data[16:24])


def make_inline(rel_id: str, drawing_id: int, filename: str, description: str, image_data: bytes) -> etree._Element:
    width_px, height_px = png_dimensions(image_data)
    width_emu = 5_029_200  # 5.5 in; fits the thesis text block on A4.
    height_emu = round(width_emu * height_px / width_px)

    drawing = etree.Element(qn(W_NS, "drawing"))
    inline = etree.SubElement(drawing, qn(WP_NS, "inline"), distT="0", distB="0", distL="0", distR="0")
    etree.SubElement(inline, qn(WP_NS, "extent"), cx=str(width_emu), cy=str(height_emu))
    etree.SubElement(inline, qn(WP_NS, "effectExtent"), l="0", t="0", r="0", b="0")
    etree.SubElement(inline, qn(WP_NS, "docPr"), id=str(drawing_id), name=f"Figure {drawing_id}", descr=description)
    etree.SubElement(inline, qn(WP_NS, "cNvGraphicFramePr"))

    graphic = etree.SubElement(inline, qn(A_NS, "graphic"))
    graphic_data = etree.SubElement(graphic, qn(A_NS, "graphicData"), uri="http://schemas.openxmlformats.org/drawingml/2006/picture")
    picture = etree.SubElement(graphic_data, qn(PIC_NS, "pic"))
    nv_pic_pr = etree.SubElement(picture, qn(PIC_NS, "nvPicPr"))
    etree.SubElement(nv_pic_pr, qn(PIC_NS, "cNvPr"), id="0", name=filename, descr=description)
    etree.SubElement(nv_pic_pr, qn(PIC_NS, "cNvPicPr"))

    blip_fill = etree.SubElement(picture, qn(PIC_NS, "blipFill"))
    etree.SubElement(blip_fill, qn(A_NS, "blip"), {qn(R_NS, "embed"): rel_id})
    stretch = etree.SubElement(blip_fill, qn(A_NS, "stretch"))
    etree.SubElement(stretch, qn(A_NS, "fillRect"))

    shape_pr = etree.SubElement(picture, qn(PIC_NS, "spPr"))
    transform = etree.SubElement(shape_pr, qn(A_NS, "xfrm"))
    etree.SubElement(transform, qn(A_NS, "off"), x="0", y="0")
    etree.SubElement(transform, qn(A_NS, "ext"), cx=str(width_emu), cy=str(height_emu))
    geometry = etree.SubElement(shape_pr, qn(A_NS, "prstGeom"), prst="rect")
    etree.SubElement(geometry, qn(A_NS, "avLst"))
    return drawing


def replace_with_drawing(paragraph: etree._Element, drawing: etree._Element) -> None:
    for child in list(paragraph):
        if child.tag != qn(W_NS, "pPr"):
            paragraph.remove(child)
    run = etree.SubElement(paragraph, qn(W_NS, "r"))
    run.append(drawing)


def next_relationship_id(relationships: etree._Element) -> int:
    identifiers = []
    for value in relationships.xpath("/*/*[local-name()='Relationship']/@Id"):
        if value.startswith("rId") and value[3:].isdigit():
            identifiers.append(int(value[3:]))
    return max(identifiers, default=0) + 1


def next_drawing_id(document: etree._Element) -> int:
    identifiers = [int(value) for value in document.xpath("//wp:docPr/@id", namespaces=NSMAP) if value.isdigit()]
    return max(identifiers, default=0) + 1


def ensure_png_content_type(content_types: etree._Element) -> None:
    for default in content_types.xpath("/*[local-name()='Default']"):
        if default.get("Extension", "").lower() == "png":
            return
    etree.SubElement(content_types, qn(CT_NS, "Default"), Extension="png", ContentType="image/png")


def main() -> None:
    if not DOCX.is_file():
        raise FileNotFoundError(f"Final thesis file not found: {DOCX}")
    for source_name, _, _ in SLOTS:
        if not (FIGURES / source_name).is_file():
            raise FileNotFoundError(f"Missing generated figure: {FIGURES / source_name}")

    fd, temporary_name = tempfile.mkstemp(prefix="thesis-figures-", suffix=".docx", dir=DOCX.parent)
    os.close(fd)
    temporary = Path(temporary_name)
    inserted = 0

    try:
        with zipfile.ZipFile(DOCX, "r") as source:
            document = etree.fromstring(source.read("word/document.xml"))
            rels = etree.fromstring(source.read("word/_rels/document.xml.rels"))
            content_types = etree.fromstring(source.read("[Content_Types].xml"))
            ensure_png_content_type(content_types)

            # Figure placeholders are stored in the template's nested layout
            # containers, so search all document paragraphs rather than only
            # direct children of w:body.
            available = list(document.xpath("//w:p", namespaces=NSMAP))
            # Each legacy image marker begins with «[محل درج شکل]».  Checking
            # the code point of the first Persian letter avoids a false match
            # with numeric bibliography entries such as «[۱] ...».
            placeholders = [
                paragraph
                for paragraph in available
                if text_of(paragraph).startswith("[")
                and len(text_of(paragraph)) > 2
                and ord(text_of(paragraph)[1]) == 0x0645
            ]
            if len(placeholders) != len(SLOTS):
                raise RuntimeError(f"Expected {len(SLOTS)} image placeholders; found {len(placeholders)}")
            relation_number = next_relationship_id(rels)
            drawing_number = next_drawing_id(document)
            new_media: dict[str, bytes] = {}

            for paragraph, (source_name, media_name, description) in zip(placeholders, SLOTS, strict=True):
                image_data = (FIGURES / source_name).read_bytes()
                rel_id = f"rId{relation_number}"
                relation_number += 1
                etree.SubElement(
                    rels,
                    qn(REL_NS, "Relationship"),
                    Id=rel_id,
                    Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image",
                    Target=f"media/{media_name}",
                )
                drawing = make_inline(rel_id, drawing_number, media_name, description, image_data)
                drawing_number += 1
                replace_with_drawing(paragraph, drawing)
                new_media[f"word/media/{media_name}"] = image_data
                inserted += 1

            with zipfile.ZipFile(temporary, "w", zipfile.ZIP_DEFLATED) as destination:
                for item in source.infolist():
                    if item.filename == "word/document.xml":
                        destination.writestr(item, etree.tostring(document, xml_declaration=True, encoding="UTF-8", standalone=True))
                    elif item.filename == "word/_rels/document.xml.rels":
                        destination.writestr(item, etree.tostring(rels, xml_declaration=True, encoding="UTF-8", standalone=True))
                    elif item.filename == "[Content_Types].xml":
                        destination.writestr(item, etree.tostring(content_types, xml_declaration=True, encoding="UTF-8", standalone=True))
                    else:
                        destination.writestr(item, source.read(item.filename))
                for media_name, data in new_media.items():
                    destination.writestr(media_name, data)

        os.replace(temporary, DOCX)
    finally:
        if temporary.exists():
            temporary.unlink()

    if inserted != len(SLOTS):
        raise RuntimeError(f"Inserted {inserted} figure(s), expected {len(SLOTS)}")
    print(f"Inserted {inserted} diagrams into {DOCX.name}.")


if __name__ == "__main__":
    main()
