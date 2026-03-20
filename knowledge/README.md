# Knowledge

Use this folder for normalized, durable business and product knowledge.

Good candidates:

- exported docs from Google Docs / Drive
- cleaned summaries of PDFs and specs
- process notes that should be searchable later
- one canonical markdown version of important reference material

Recommended metadata block at the top of imported files:

- `title`
- `source`
- `file_id`
- `mime_type`
- `modified_time`
- `url`
- `trust_level`

Avoid:

- raw dumps with no summary
- duplicate copies of the same document
- secrets or credentials

Suggested layout:

- `knowledge/drive/` for imported Google Drive / Docs files
- `knowledge/manual/` for manually curated durable notes
