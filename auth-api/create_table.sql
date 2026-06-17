-- 系统账号表（公卫账号、人口网账号、社区通账号等，记录登录信息）
CREATE TABLE `rpa_account`(
	`id` INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
	`type` VARCHAR(100) NOT NULL COMMENT '账号类型',
	`user_name` VARCHAR(100) NOT NULL COMMENT '账号',
	`password` VARCHAR(100) NOT NULL COMMENT '密码',
	`rolename` VARCHAR(100) NOT NULL DEFAULT "" COMMENT '账号角色',
	`nickname` VARCHAR(100) NOT NULL DEFAULT "" COMMENT '账号昵称',
	`token` VARCHAR(500) NOT NULL DEFAULT "" COMMENT '登录token',
	`login_data` TEXT COMMENT '登录信息',
	`status` INT DEFAULT 1 COMMENT '状态：0-无效，1-有效',
	`created_time` DATETIME DEFAULT CURRENT_TIMESTAMP,
	`updated_time` DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE = INNODB CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT '系统账号表';


-- 客户端登录表
CREATE TABLE IF NOT EXISTS hospital_clients (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  hospital_code VARCHAR(64) NOT NULL COMMENT '医院编号',
  username VARCHAR(64) NOT NULL COMMENT '登录账号',
  password_hash CHAR(64) NOT NULL COMMENT 'SHA256密码哈希',
  role VARCHAR(32) NOT NULL DEFAULT 'user' COMMENT '角色权限：user/super_admin',
  enabled TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否启用',
  expires_at DATETIME NULL COMMENT '授权过期时间',
  remark VARCHAR(255) DEFAULT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;


-- 
CREATE TABLE IF NOT EXISTS hospital_business_tokens (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  hospital_code VARCHAR(64) NOT NULL COMMENT '医院编号',
  business_token TEXT NOT NULL COMMENT '业务流程使用的token',
  enabled TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否启用',
  expires_at DATETIME NULL COMMENT 'token过期时间',
  last_used_at DATETIME NULL COMMENT '最后使用时间',
  remark VARCHAR(255) DEFAULT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_hospital_code (hospital_code),
  KEY idx_enabled_expires_at (enabled, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 示例账号，密码是 123456，上线前请删除或改掉。
INSERT INTO hospital_clients (
  hospital_code,
  username,
  password_hash,
  role,
  enabled,  
  expires_at,
  remark
) VALUES (
  '天河社区卫生院',
  'admin',
  SHA2('123456', 256),
  'super_admin',
  1,
  NULL,
  '超管账号'
);

INSERT INTO hospital_clients (
  hospital_code,
  username,
  password_hash,
  role,
  enabled,
  expires_at,
  remark
) VALUES (
  '天河社区卫生院1',
  'admin1',
  SHA2('1234567', 256),
  'user',
  1,
  NULL,
  '测试账号'
);

INSERT INTO hospital_business_tokens (
  hospital_code,
  business_token,
  enabled,
  expires_at,
  remark
) VALUES (
  '天河社区卫生院',
  'replace_with_real_business_token',
  1,
  NULL,
  '测试业务token'
);

INSERT INTO hospital_business_tokens (
  hospital_code,
  business_token,
  enabled,
  expires_at,
  remark
) VALUES (
  '天河社区卫生院1',
  '',
  1,
  NULL,
  '测试账号业务token，登录后更新'
);