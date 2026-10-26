CREATE TABLE users (
    id            BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    email         VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    name          VARCHAR(255) NOT NULL,
    created_at    DATETIME(6)  NOT NULL,
    UNIQUE KEY uq_users_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE teams (
    id         BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    created_by BIGINT       NOT NULL,
    created_at DATETIME(6)  NOT NULL,
    CONSTRAINT fk_teams_created_by FOREIGN KEY (created_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE team_members (
    team_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    role    ENUM('owner', 'admin', 'member') NOT NULL DEFAULT 'member',
    PRIMARY KEY (team_id, user_id),
    KEY idx_team_members_user (user_id),
    CONSTRAINT fk_tm_team FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE,
    CONSTRAINT fk_tm_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE tasks (
    id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    team_id     BIGINT       NOT NULL,
    title       VARCHAR(255) NOT NULL,
    description TEXT         NOT NULL,
    status      ENUM('todo', 'in_progress', 'done') NOT NULL DEFAULT 'todo',
    created_by  BIGINT       NOT NULL,
    assignee_id BIGINT       NULL,
    created_at  DATETIME(6)  NOT NULL,
    updated_at  DATETIME(6)  NOT NULL,
    closed_at   DATETIME(6)  NULL,
    version     INT          NOT NULL DEFAULT 1,
    KEY idx_tasks_team_status_assignee (team_id, status, assignee_id),
    KEY idx_tasks_team_created (team_id, created_at),
    KEY idx_tasks_team_closed (team_id, status, closed_at),
    CONSTRAINT fk_tasks_team     FOREIGN KEY (team_id)     REFERENCES teams (id) ON DELETE CASCADE,
    CONSTRAINT fk_tasks_creator  FOREIGN KEY (created_by)  REFERENCES users (id),
    CONSTRAINT fk_tasks_assignee FOREIGN KEY (assignee_id) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE task_history (
    id         BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    task_id    BIGINT      NOT NULL,
    changed_by BIGINT      NOT NULL,
    changes    JSON        NOT NULL,
    created_at DATETIME(6) NOT NULL,
    KEY idx_history_task (task_id, id),
    CONSTRAINT fk_history_task FOREIGN KEY (task_id)    REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_history_user FOREIGN KEY (changed_by) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE task_comments (
    id         BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    task_id    BIGINT      NOT NULL,
    user_id    BIGINT      NOT NULL,
    content    TEXT        NOT NULL,
    created_at DATETIME(6) NOT NULL,
    KEY idx_comments_task (task_id, id),
    CONSTRAINT fk_comments_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_comments_user FOREIGN KEY (user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
