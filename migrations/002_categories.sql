-- Add missing columns to older expenses tables.
-- The migration runner skips columns that already exist.

ALTER TABLE expenses ADD COLUMN expense_date TEXT NOT NULL DEFAULT '';

ALTER TABLE expenses ADD COLUMN user_id INTEGER NOT NULL DEFAULT 1;

ALTER TABLE expenses ADD COLUMN category_id INTEGER REFERENCES categories(id);

-- Insert default categories.
INSERT OR IGNORE INTO categories(name)
VALUES
    ('Еда'),
    ('Транспорт'),
    ('Жильё'),
    ('Развлечения'),
    ('Здоровье'),
    ('Покупки'),
    ('Другое');

-- Preserve older category names.
INSERT OR IGNORE INTO categories(name)
SELECT DISTINCT trim(category)
FROM expenses
WHERE trim(category) <> '';

-- Link expenses to their categories.
UPDATE expenses
SET category_id = (
    SELECT id
    FROM categories
    WHERE categories.name = trim(expenses.category) COLLATE NOCASE
)
WHERE category_id IS NULL;