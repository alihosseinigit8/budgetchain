"""Apply the university heading-alignment rule to the single final thesis file.

Only chapter-opening titles ("فصل اول" ... "فصل پنجم") are physically
left-aligned.  All other headings, including numbered sections, are
right-aligned and black.  The script edits just the relevant XML parts of the
existing DOCX package, so footnotes, images, tables, and all other content are
preserved.
"""

from __future__ import annotations

import os
import re
import shutil
import tempfile
import zipfile
from pathlib import Path

from lxml import etree


DOCX = Path(__file__).resolve().parents[1] / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"
NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
W = f"{{{NS}}}"
NSMAP = {"w": NS}

CHAPTER_RE = re.compile(r"^فصل\s*(اول|دوم|سوم|چهارم|پنجم)")
NUMBERED_HEADING_RE = re.compile(r"^[۰-۹0-9]+(?:[-–][۰-۹0-9]+)+(?:[.،:])?")


def text_of(paragraph: etree._Element) -> str:
    return "".join(paragraph.xpath(".//w:t/text()", namespaces=NSMAP)).strip()


def paragraph_properties(paragraph: etree._Element) -> etree._Element:
    props = paragraph.find(f"{W}pPr")
    if props is None:
        props = etree.Element(f"{W}pPr")
        paragraph.insert(0, props)
    return props


def set_child_value(parent: etree._Element, tag: str, value: str) -> None:
    child = parent.find(f"{W}{tag}")
    if child is None:
        child = etree.SubElement(parent, f"{W}{tag}")
    child.set(f"{W}val", value)


def set_visible_alignment(paragraph: etree._Element, alignment: str) -> None:
    """Set the physical alignment shown by Word in this RTL document.

    The template has ``w:bidi`` enabled.  Word consequently mirrors the
    OOXML left/right values, so a visible left alignment is encoded as
    ``w:jc=right`` and a visible right alignment as ``w:jc=left``.
    """
    props = paragraph_properties(paragraph)
    encoded_alignment = "right" if alignment == "left" else "left"
    set_child_value(props, "jc", encoded_alignment)
    if props.find(f"{W}bidi") is None:
        etree.SubElement(props, f"{W}bidi")


def blacken_runs(paragraph: etree._Element) -> None:
    for run in paragraph.xpath(".//w:r", namespaces=NSMAP):
        rpr = run.find(f"{W}rPr")
        if rpr is None:
            rpr = etree.Element(f"{W}rPr")
            run.insert(0, rpr)
        set_child_value(rpr, "color", "000000")


def set_font(rpr: etree._Element, name: str, half_points: int | None = None) -> None:
    """Apply a Word font to every script used by the selected run/style."""
    fonts = rpr.find(f"{W}rFonts")
    if fonts is None:
        fonts = etree.SubElement(rpr, f"{W}rFonts")
    for attribute in ("ascii", "hAnsi", "eastAsia", "cs"):
        fonts.set(f"{W}{attribute}", name)
    if half_points is not None:
        set_child_value(rpr, "sz", str(half_points))
        set_child_value(rpr, "szCs", str(half_points))


def use_b_nazanin(paragraph: etree._Element, half_points: int | None = None) -> None:
    for run in paragraph.xpath(".//w:r", namespaces=NSMAP):
        rpr = run.find(f"{W}rPr")
        if rpr is None:
            rpr = etree.Element(f"{W}rPr")
            run.insert(0, rpr)
        set_font(rpr, "B Nazanin", half_points)


def style_id(paragraph: etree._Element) -> str:
    value = paragraph.xpath("./w:pPr/w:pStyle/@w:val", namespaces=NSMAP)
    return value[0] if value else ""


def has_page_break_before(paragraph: etree._Element) -> bool:
    return paragraph.find(f"{W}pPr/{W}pageBreakBefore") is not None


def remove_direct_left_alignment(paragraph: etree._Element) -> None:
    """Undo a previous broad chapter-title match for normal body paragraphs."""
    props = paragraph.find(f"{W}pPr")
    if props is None:
        return
    alignment = props.find(f"{W}jc")
    if alignment is not None and alignment.get(f"{W}val") == "left":
        props.remove(alignment)


def is_heading_style(value: str) -> bool:
    normalized = value.lower().replace(" ", "")
    return normalized.startswith("heading")


def make_relevant_styles_black_and_visibly_right(styles: etree._Element) -> None:
    for style in styles.xpath("//w:style", namespaces=NSMAP):
        style_id_value = style.get(f"{W}styleId", "")
        style_name = style.xpath("./w:name/@w:val", namespaces=NSMAP)
        name_value = style_name[0] if style_name else ""
        relevant = is_heading_style(style_id_value) or is_heading_style(name_value)
        relevant = relevant or style_id_value == "Caption1" or name_value in {"Caption", "Caption1"}
        if not relevant:
            continue
        rpr = style.find(f"{W}rPr")
        if rpr is None:
            rpr = etree.SubElement(style, f"{W}rPr")
        set_child_value(rpr, "color", "000000")
        # Captions are centred by design; only structural headings are right-aligned.
        if is_heading_style(style_id_value) or is_heading_style(name_value):
            ppr = style.find(f"{W}pPr")
            if ppr is None:
                ppr = etree.SubElement(style, f"{W}pPr")
            # ``w:bidi`` is enabled in the template, so ``left`` displays on
            # the physical right side of the page.
            set_child_value(ppr, "jc", "left")
            if ppr.find(f"{W}bidi") is None:
                etree.SubElement(ppr, f"{W}bidi")
            set_font(rpr, "B Nazanin")


def main() -> None:
    if not DOCX.is_file():
        raise FileNotFoundError(f"Final thesis file not found: {DOCX}")

    fd, temporary_name = tempfile.mkstemp(prefix="thesis-heading-fix-", suffix=".docx", dir=DOCX.parent)
    os.close(fd)
    temporary = Path(temporary_name)
    try:
        with zipfile.ZipFile(DOCX, "r") as source:
            document = etree.fromstring(source.read("word/document.xml"))
            styles = etree.fromstring(source.read("word/styles.xml"))

            chapter_count = 0
            right_aligned_count = 0
            for paragraph in document.xpath("//w:body/w:p", namespaces=NSMAP):
                content = text_of(paragraph)
                if not content:
                    continue

                # The five genuine chapter openings have a page break before
                # them.  This deliberately excludes ordinary body sentences
                # such as «فصل سوم نشان داد...» and the table of contents.
                chapter_like = bool(CHAPTER_RE.match(content))
                chapter_opening = chapter_like and has_page_break_before(paragraph)
                if chapter_opening:
                    set_visible_alignment(paragraph, "left")
                    blacken_runs(paragraph)
                    # The template's chapter opening uses B Nazanin at 18 pt.
                    use_b_nazanin(paragraph, half_points=36)
                    chapter_count += 1
                    continue

                if chapter_like:
                    remove_direct_left_alignment(paragraph)

                if is_heading_style(style_id(paragraph)) or NUMBERED_HEADING_RE.match(content):
                    set_visible_alignment(paragraph, "right")
                    blacken_runs(paragraph)
                    # Keep the existing heading size, but use B Nazanin for all
                    # structural and numbered headings such as «۲-۱. مقدمه».
                    use_b_nazanin(paragraph)
                    right_aligned_count += 1

            make_relevant_styles_black_and_visibly_right(styles)

            with zipfile.ZipFile(temporary, "w", zipfile.ZIP_DEFLATED) as destination:
                for item in source.infolist():
                    if item.filename == "word/document.xml":
                        destination.writestr(item, etree.tostring(document, xml_declaration=True, encoding="UTF-8", standalone=True))
                    elif item.filename == "word/styles.xml":
                        destination.writestr(item, etree.tostring(styles, xml_declaration=True, encoding="UTF-8", standalone=True))
                    else:
                        destination.writestr(item, source.read(item.filename))
        # The input archive must be closed before its path can be atomically
        # replaced on Windows.
        os.replace(temporary, DOCX)
    finally:
        if temporary.exists():
            temporary.unlink()

    print(f"Updated {DOCX.name}: {chapter_count} chapter openings left; {right_aligned_count} other headings right.")


if __name__ == "__main__":
    main()
