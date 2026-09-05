"""Insert first-use explanatory footnotes for English abbreviations in the thesis.

For Persian academic writing, the ordinary convention is to define an acronym
at its first meaningful occurrence and use its shortened form thereafter.
The thesis also has an abbreviations list; these footnotes make the first use
self-contained without repeating the same explanation throughout the text.
"""

from __future__ import annotations

import copy
import os
import re
import zipfile
from pathlib import Path

from lxml import etree


WORKSPACE = Path(__file__).resolve().parents[1]
INPUT = Path(os.environ.get(
    "THESIS_INPUT",
    str(WORKSPACE / "BudgetChain_BSc_Thesis_University_Style.docx"),
))
OUTPUT = Path(os.environ.get(
    "THESIS_OUTPUT",
    str(WORKSPACE / "BudgetChain_BSc_Thesis_University_Style_Footnotes.docx"),
))

W = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
R = "http://schemas.openxmlformats.org/package/2006/relationships"
CT = "http://schemas.openxmlformats.org/package/2006/content-types"
NS = {"w": W, "r": R, "ct": CT}


# Only abbreviations used in the thesis prose are included.  Proper product
# names and ordinary English technical terms remain in the text without an
# unnecessary footnote.
DEFINITIONS = {
    "NIST": (
        "National Institute of Standards and Technology (NIST): "
        "مؤسسهٔ ملی استانداردها و فناوری ایالات متحدهٔ آمریکا."
    ),
    "NISTIR": (
        "NIST Interagency or Internal Report (NISTIR): "
        "گزارش بین‌سازمانی یا داخلی مؤسسهٔ ملی استانداردها و فناوری ایالات متحدهٔ آمریکا."
    ),
    "OECD": (
        "Organisation for Economic Co-operation and Development (OECD): "
        "سازمان همکاری و توسعهٔ اقتصادی."
    ),
    "ECDSA": (
        "Elliptic Curve Digital Signature Algorithm (ECDSA): "
        "الگوریتم امضای دیجیتال مبتنی بر منحنی بیضوی."
    ),
    "PBFT": (
        "Practical Byzantine Fault Tolerance (PBFT): "
        "سازوکار تحمل خطای عملی بیزانسی."
    ),
    "FIPS": (
        "Federal Information Processing Standard (FIPS): "
        "استاندارد پردازش اطلاعات فدرال آمریکا."
    ),
    "PoC": (
        "Proof of Concept (PoC): "
        "نمونهٔ مفهومی؛ پیاده‌سازی اولیه‌ای برای نشان‌دادن امکان‌پذیری یک ایده."
    ),
    "EVM": (
        "Ethereum Virtual Machine (EVM): "
        "ماشین مجازی اجرای قراردادهای هوشمند در شبکهٔ اتریوم."
    ),
    "TxRoot": (
        "Transaction Root (TxRoot): "
        "ریشهٔ هشِ مجموعهٔ تراکنش‌های یک بلاک."
    ),
    "API": (
        "Application Programming Interface (API): "
        "رابط برنامه‌نویسی کاربردی برای تبادل داده میان سامانه‌ها."
    ),
}


def qn(name: str) -> str:
    prefix, local = name.split(":", 1)
    return f"{{{W if prefix == 'w' else R}}}{local}"


def paragraph_text(paragraph: etree._Element) -> str:
    return "".join(paragraph.xpath(".//w:t/text()", namespaces=NS))


def in_table(paragraph: etree._Element) -> bool:
    return any(ancestor.tag == qn("w:tbl") for ancestor in paragraph.iterancestors())


def compile_term(term: str) -> re.Pattern[str]:
    return re.compile(rf"(?<![A-Za-z0-9]){re.escape(term)}(?![A-Za-z0-9])")


def find_first_occurrences(document: etree._Element) -> list[tuple[int, str]]:
    paragraphs = document.xpath(".//w:body//w:p", namespaces=NS)
    first_chapter = next(
        (index for index, paragraph in enumerate(paragraphs)
         if paragraph_text(paragraph).replace("\n", " ").strip().startswith("فصل اول")
         and "مقدمه" in paragraph_text(paragraph)),
        None,
    )
    if first_chapter is None:
        raise ValueError("The first chapter heading was not found.")

    locations: list[tuple[int, str]] = []
    for term in DEFINITIONS:
        matcher = compile_term(term)
        for index, paragraph in enumerate(paragraphs[first_chapter + 1:], start=first_chapter + 1):
            if in_table(paragraph):
                continue
            if matcher.search(paragraph_text(paragraph)):
                locations.append((index, term))
                break
        else:
            raise ValueError(f"No prose occurrence was found for abbreviation: {term}")
    return sorted(locations)


def create_footnote_root() -> etree._Element:
    root = etree.Element(qn("w:footnotes"), nsmap={"w": W})
    separator = etree.SubElement(root, qn("w:footnote"), {qn("w:id"): "-1", qn("w:type"): "separator"})
    separator_p = etree.SubElement(separator, qn("w:p"))
    separator_r = etree.SubElement(separator_p, qn("w:r"))
    etree.SubElement(separator_r, qn("w:separator"))
    continuation = etree.SubElement(
        root, qn("w:footnote"), {qn("w:id"): "0", qn("w:type"): "continuationSeparator"}
    )
    continuation_p = etree.SubElement(continuation, qn("w:p"))
    continuation_r = etree.SubElement(continuation_p, qn("w:r"))
    etree.SubElement(continuation_r, qn("w:continuationSeparator"))
    return root


def add_footnote(footnotes: etree._Element, footnote_id: int, text: str) -> None:
    footnote = etree.SubElement(footnotes, qn("w:footnote"), {qn("w:id"): str(footnote_id)})
    paragraph = etree.SubElement(footnote, qn("w:p"))
    ppr = etree.SubElement(paragraph, qn("w:pPr"))
    etree.SubElement(ppr, qn("w:bidi"), {qn("w:val"): "1"})
    marker_run = etree.SubElement(paragraph, qn("w:r"))
    marker_properties = etree.SubElement(marker_run, qn("w:rPr"))
    etree.SubElement(marker_properties, qn("w:rStyle"), {qn("w:val"): "FootnoteReference"})
    etree.SubElement(marker_run, qn("w:footnoteRef"))
    run = etree.SubElement(paragraph, qn("w:r"))
    rpr = etree.SubElement(run, qn("w:rPr"))
    etree.SubElement(
        rpr,
        qn("w:rFonts"),
        {
            qn("w:ascii"): "Times New Roman",
            qn("w:hAnsi"): "Times New Roman",
            qn("w:cs"): "B Nazanin",
        },
    )
    etree.SubElement(rpr, qn("w:sz"), {qn("w:val"): "20"})
    etree.SubElement(rpr, qn("w:szCs"), {qn("w:val"): "20"})
    etree.SubElement(rpr, qn("w:rtl"), {qn("w:val"): "1"})
    etree.SubElement(rpr, qn("w:lang"), {qn("w:val"): "en-US", qn("w:bidi"): "fa-IR"})
    t = etree.SubElement(run, qn("w:t"))
    t.text = text


def clone_run_with_text(run: etree._Element, text: str) -> etree._Element:
    clone = copy.deepcopy(run)
    for child in list(clone):
        if child.tag != qn("w:rPr"):
            clone.remove(child)
    t = etree.SubElement(clone, qn("w:t"))
    if text.startswith(" ") or text.endswith(" "):
        t.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")
    t.text = text
    return clone


def insert_reference(paragraph: etree._Element, term: str, footnote_id: int) -> None:
    matcher = compile_term(term)
    for text_node in paragraph.xpath(".//w:t", namespaces=NS):
        text = text_node.text or ""
        match = matcher.search(text)
        if match is None:
            continue
        run = text_node.getparent()
        if run.tag != qn("w:r"):
            continue
        before = text[:match.end()]
        after = text[match.end():]
        text_node.text = before
        footnote_run = etree.Element(qn("w:r"))
        footnote_properties = etree.SubElement(footnote_run, qn("w:rPr"))
        etree.SubElement(footnote_properties, qn("w:rStyle"), {qn("w:val"): "FootnoteReference"})
        footnote_ref = etree.SubElement(footnote_run, qn("w:footnoteReference"))
        footnote_ref.set(qn("w:id"), str(footnote_id))
        insertion_at = run.getparent().index(run) + 1
        run.getparent().insert(insertion_at, footnote_run)
        if after:
            run.getparent().insert(insertion_at + 1, clone_run_with_text(run, after))
        return
    raise ValueError(f"Could not insert footnote reference for {term}.")


def ensure_footnote_parts(parts: dict[str, bytes]) -> etree._Element:
    if "word/footnotes.xml" in parts:
        return etree.fromstring(parts["word/footnotes.xml"])
    return create_footnote_root()


def add_footnote_relationship(parts: dict[str, bytes]) -> None:
    rels_name = "word/_rels/document.xml.rels"
    root = etree.fromstring(parts[rels_name])
    if root.xpath("./rel:Relationship[@Type='http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes']", namespaces={"rel": R}):
        parts[rels_name] = etree.tostring(root, xml_declaration=True, encoding="UTF-8", standalone=True)
        return
    ids = [
        int(item.get("Id")[3:])
        for item in root.xpath("./rel:Relationship", namespaces={"rel": R})
        if (item.get("Id") or "").startswith("rId") and (item.get("Id")[3:]).isdigit()
    ]
    etree.SubElement(
        root,
        f"{{{R}}}Relationship",
        {
            "Id": f"rId{max(ids, default=0) + 1}",
            "Type": "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes",
            "Target": "footnotes.xml",
        },
    )
    parts[rels_name] = etree.tostring(root, xml_declaration=True, encoding="UTF-8", standalone=True)


def add_content_type(parts: dict[str, bytes]) -> None:
    name = "[Content_Types].xml"
    root = etree.fromstring(parts[name])
    existing = root.xpath("./ct:Override[@PartName='/word/footnotes.xml']", namespaces=NS)
    if not existing:
        etree.SubElement(
            root,
            f"{{{CT}}}Override",
            {
                "PartName": "/word/footnotes.xml",
                "ContentType": "application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml",
            },
        )
    parts[name] = etree.tostring(root, xml_declaration=True, encoding="UTF-8", standalone=True)


def main() -> None:
    if not INPUT.exists():
        raise FileNotFoundError(f"Input thesis not found: {INPUT}")
    if OUTPUT.exists():
        raise FileExistsError(f"Refusing to overwrite existing output: {OUTPUT}")

    with zipfile.ZipFile(INPUT, "r") as source:
        parts = {name: source.read(name) for name in source.namelist()}

    document = etree.fromstring(parts["word/document.xml"])
    paragraphs = document.xpath(".//w:body//w:p", namespaces=NS)
    targets = find_first_occurrences(document)
    footnotes = ensure_footnote_parts(parts)
    next_id = max(
        (int(note.get(qn("w:id"))) for note in footnotes.xpath("./w:footnote", namespaces=NS)),
        default=0,
    ) + 1

    added: list[str] = []
    for paragraph_index, term in targets:
        insert_reference(paragraphs[paragraph_index], term, next_id)
        add_footnote(footnotes, next_id, DEFINITIONS[term])
        added.append(term)
        next_id += 1

    parts["word/document.xml"] = etree.tostring(document, xml_declaration=True, encoding="UTF-8", standalone=True)
    parts["word/footnotes.xml"] = etree.tostring(footnotes, xml_declaration=True, encoding="UTF-8", standalone=True)
    add_footnote_relationship(parts)
    add_content_type(parts)

    with zipfile.ZipFile(OUTPUT, "w", compression=zipfile.ZIP_DEFLATED) as destination:
        for name, value in parts.items():
            destination.writestr(name, value)

    print(f"THESIS_CREATED={OUTPUT}")
    print(f"FIRST_USE_FOOTNOTES={','.join(added)}")


if __name__ == "__main__":
    main()
