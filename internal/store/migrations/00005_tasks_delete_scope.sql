-- +goose Up
-- DELETE routes now need the tasks:delete scope (ADR-0015). Existing tokens
-- that could write tasks keep the delete power they already had.
UPDATE api_tokens
SET scopes = scopes || ' tasks:delete'
WHERE (' ' || scopes || ' ') LIKE '% tasks:write %'
  AND (' ' || scopes || ' ') NOT LIKE '% tasks:delete %';

-- +goose Down
UPDATE api_tokens
SET scopes = trim(replace(' ' || scopes || ' ', ' tasks:delete ', ' '))
WHERE (' ' || scopes || ' ') LIKE '% tasks:delete %';
