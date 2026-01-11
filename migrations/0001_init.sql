create table if not exists users(
  id bigserial primary key,
  telegram_user_id bigint not null,
  chat_id bigint not null,
  timezone text not null default 'UTC',
  created_at timestamptz not null default now()
);

create unique index if not exists users_telegram_user_id_idx on users(telegram_user_id);

create table if not exists tasks(
  id bigserial primary key,
  user_id bigint not null references users(id) on delete cascade,
  text text not null,
  status text not null default 'active' check (status in ('active', 'done')),
  due_at timestamptz,
  remind_at timestamptz,
  notified_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists tasks_user_id_idx on tasks(user_id);
create index if not exists tasks_user_id_status_idx on tasks(user_id, status);
create index if not exists tasks_remind_at_idx on tasks(remind_at);

create table if not exists attachments(
  id bigserial primary key,
  task_id bigint not null references tasks(id) on delete cascade,
  type text not null,
  telegram_file_id text not null,
  file_unique_id text not null,
  caption text
);

create index if not exists attachments_task_id_idx on attachments(task_id);
