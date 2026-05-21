-- Team identity (Design UX/UI track): cada time ganha cor + emoji opcionais.
-- Não impõe regra de unicidade de cor/emoji entre times do mesmo tenant —
-- a UI vai sugerir variações na criação mas o operador decide.

ALTER TABLE platform.team
  ADD COLUMN color TEXT,
  ADD COLUMN emoji TEXT,
  ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();

COMMENT ON COLUMN platform.team.color IS 'Hex CSS color, ex: #2563eb. Usado em chips/cards no dashboard.';
COMMENT ON COLUMN platform.team.emoji IS 'Emoji curto (1-2 caracteres) usado como mini-avatar.';
