-- Dropping these destroys issued legal documents, which French law requires to
-- be retained for 10 years. This down migration exists to make the up
-- reversible in development; it must never be run against production data that
-- carries real invoices.
DROP TABLE IF EXISTS invoice_lines;
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS invoice_sequences;
