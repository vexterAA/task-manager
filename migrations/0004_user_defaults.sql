alter table users
  add column if not exists default_remind_kind text,
  add column if not exists default_remind_interval text;
