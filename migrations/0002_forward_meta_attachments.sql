alter table tasks
  add column if not exists forward_meta jsonb;

alter table attachments
  add column if not exists mime_type text,
  add column if not exists file_name text,
  add column if not exists file_size bigint;
