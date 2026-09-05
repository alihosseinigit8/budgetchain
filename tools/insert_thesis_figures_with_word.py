"""Use Microsoft Word itself to replace stable figure markers with PNG diagrams."""

from __future__ import annotations

from pathlib import Path

import win32com.client


ROOT = Path(__file__).resolve().parents[1]
DOCX = ROOT / "BudgetChain_BSc_Thesis_University_Style_Footnotes_Final.docx"
FIGURES = ROOT / "assets" / "thesis_figures"
SLOTS = (
    ("[[FIGURE_2_1]]", "figure-2-1-workflow.png"),
    ("[[FIGURE_3_1]]", "figure-3-1-four-ledgers.png"),
    ("[[FIGURE_3_2]]", "figure-3-3-tax-boundary.png"),
    ("[[FIGURE_3_3]]", "figure-3-2-sequence.png"),
    ("[[FIGURE_4_1]]", "figure-4-1-demo-flow.png"),
)
WD_FIND_STOP = 0
WD_ALIGN_PARAGRAPH_CENTER = 1
WD_DO_NOT_SAVE_CHANGES = 0


def find_marker(document, marker: str):
    search_range = document.Content
    finder = search_range.Find
    finder.ClearFormatting()
    finder.Replacement.ClearFormatting()
    finder.MatchWildcards = False
    found = finder.Execute(FindText=marker, Forward=True, Wrap=WD_FIND_STOP, Format=False)
    if not found:
        raise RuntimeError(f"Could not find figure marker {marker}")
    return search_range


def main() -> None:
    if not DOCX.is_file():
        raise FileNotFoundError(f"Final thesis file not found: {DOCX}")
    for _, filename in SLOTS:
        if not (FIGURES / filename).is_file():
            raise FileNotFoundError(f"Missing figure asset: {FIGURES / filename}")

    word = None
    document = None
    inserted = 0
    try:
        word = win32com.client.DispatchEx("Word.Application")
        word.Visible = False
        word.DisplayAlerts = 0
        document = word.Documents.Open(
            str(DOCX),
            ConfirmConversions=False,
            ReadOnly=False,
            AddToRecentFiles=False,
            Visible=False,
        )
        for marker, filename in SLOTS:
            marker_range = find_marker(document, marker)
            image = document.InlineShapes.AddPicture(
                FileName=str(FIGURES / filename),
                LinkToFile=False,
                SaveWithDocument=True,
                Range=marker_range,
            )
            image.Range.ParagraphFormat.Alignment = WD_ALIGN_PARAGRAPH_CENTER
            inserted += 1
        document.Save()
    finally:
        if document is not None:
            document.Close(SaveChanges=WD_DO_NOT_SAVE_CHANGES)
        if word is not None:
            word.Quit()
    if inserted != len(SLOTS):
        raise RuntimeError(f"Inserted {inserted} figures, expected {len(SLOTS)}")
    print(f"Inserted {inserted} figures with Microsoft Word.")


if __name__ == "__main__":
    main()
