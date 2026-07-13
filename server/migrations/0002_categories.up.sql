CREATE TABLE categories (
    id         BIGINT                    NOT NULL AUTO_INCREMENT,
    user_id    BIGINT                    NULL,
    name       VARCHAR(32)               NOT NULL,
    icon       VARCHAR(16)               NOT NULL DEFAULT '',
    kind       ENUM ('expense','income') NOT NULL,
    sort       INT                       NOT NULL DEFAULT 0,
    deleted_at DATETIME(3)               NULL,
    PRIMARY KEY (id),
    KEY idx_categories_user (user_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

-- 系统预设分类（user_id NULL），id 固定 1-11，PROTOCOL.md §3
INSERT INTO categories (id, user_id, name, icon, kind, sort)
VALUES (1, NULL, '餐饮', '🍜', 'expense', 1),
       (2, NULL, '奶茶', '🧋', 'expense', 2),
       (3, NULL, '交通', '🚇', 'expense', 3),
       (4, NULL, '购物', '🛍️', 'expense', 4),
       (5, NULL, '娱乐', '🎮', 'expense', 5),
       (6, NULL, '日用', '🧻', 'expense', 6),
       (7, NULL, '医疗', '💊', 'expense', 7),
       (8, NULL, '其他', '📦', 'expense', 8),
       (9, NULL, '工资', '💰', 'income', 1),
       (10, NULL, '红包', '🧧', 'income', 2),
       (11, NULL, '其他', '🪙', 'income', 3);
