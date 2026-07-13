CREATE TABLE transactions (
    id             CHAR(36)                                       NOT NULL,
    user_id        BIGINT                                         NOT NULL,
    amount_cents   BIGINT                                         NOT NULL,
    direction      ENUM ('expense','income')                      NOT NULL,
    category_id    BIGINT                                         NOT NULL,
    note           VARCHAR(500)                                   NOT NULL DEFAULT '',
    occurred_at    DATETIME(3)                                    NOT NULL,
    payment_method ENUM ('wechat','alipay','cash','card','other') NOT NULL DEFAULT 'other',
    created_at     DATETIME(3)                                    NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at     DATETIME(3)                                    NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    deleted_at     DATETIME(3)                                    NULL,
    PRIMARY KEY (id),
    KEY idx_tx_user_occurred (user_id, occurred_at),
    KEY idx_tx_user_updated (user_id, updated_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE tickets (
    id             CHAR(36)                                                NOT NULL,
    user_id        BIGINT                                                  NOT NULL,
    transaction_id CHAR(36)                                                NOT NULL,
    kind           ENUM ('movie','show','attraction','train','flight','other') NOT NULL,
    title          VARCHAR(128)                                            NOT NULL,
    venue          VARCHAR(128)                                            NOT NULL DEFAULT '',
    event_time     DATETIME(3)                                             NOT NULL,
    seat           VARCHAR(64)                                             NOT NULL DEFAULT '',
    extra          JSON                                                    NOT NULL,
    rating         TINYINT                                                 NOT NULL DEFAULT 0,
    memo           TEXT                                                    NULL,
    created_at     DATETIME(3)                                             NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at     DATETIME(3)                                             NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    deleted_at     DATETIME(3)                                             NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_tickets_transaction (transaction_id),
    KEY idx_tickets_user_kind_time (user_id, kind, event_time),
    KEY idx_tickets_user_updated (user_id, updated_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;

CREATE TABLE attachments (
    id         BIGINT       NOT NULL AUTO_INCREMENT,
    user_id    BIGINT       NOT NULL,
    ticket_id  CHAR(36)     NULL,
    file_path  VARCHAR(255) NOT NULL,
    thumb_path VARCHAR(255) NOT NULL,
    w          INT          NOT NULL DEFAULT 0,
    h          INT          NOT NULL DEFAULT 0,
    size       BIGINT       NOT NULL DEFAULT 0,
    created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_attachments_ticket (ticket_id),
    KEY idx_attachments_user (user_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_unicode_ci;
