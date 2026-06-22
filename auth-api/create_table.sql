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

-- 批量查询任务
CREATE TABLE IF NOT EXISTS batch_query_jobs (
  id BIGINT PRIMARY KEY AUTO_INCREMENT COMMENT '批量查询任务主键ID',
  hospital_code VARCHAR(64) NOT NULL COMMENT '任务所属医院编号',
  created_by VARCHAR(64) NOT NULL COMMENT '任务创建人登录账号',
  file_name VARCHAR(255) NOT NULL COMMENT '导入的原始文件名称',
  status VARCHAR(20) NOT NULL DEFAULT 'pending'
    COMMENT '任务状态：pending待开始、running查询中、paused已暂停、completed已完成、stopped已停止、failed执行失败',

  total_count INT NOT NULL DEFAULT 0 COMMENT '导入数据总数',
  pending_count INT NOT NULL DEFAULT 0 COMMENT '待查询数据数量',
  success_count INT NOT NULL DEFAULT 0 COMMENT '查询成功数据数量',
  not_found_count INT NOT NULL DEFAULT 0 COMMENT '业务系统查无此人的数据数量',
  failed_count INT NOT NULL DEFAULT 0 COMMENT '查询失败数据数量',

  worker_count INT NOT NULL DEFAULT 5 COMMENT '业务接口并发查询Worker数量',
  fetch_batch_size INT NOT NULL DEFAULT 500 COMMENT '每次从数据库领取的任务数量',
  write_batch_size INT NOT NULL DEFAULT 200 COMMENT '每次批量写入查询结果的数量',

  error_message VARCHAR(1000) DEFAULT NULL COMMENT '任务级执行错误信息',
  started_at DATETIME NULL COMMENT '任务开始查询时间',
  completed_at DATETIME NULL COMMENT '任务完成或停止时间',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '任务创建时间',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
    ON UPDATE CURRENT_TIMESTAMP COMMENT '任务最后更新时间',

  KEY idx_hospital_created (hospital_code, created_at),
  KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='居民档案批量查询任务表';

-- Excel导入的身份证查询项
CREATE TABLE IF NOT EXISTS batch_query_items (
  id BIGINT PRIMARY KEY AUTO_INCREMENT COMMENT '批量查询明细主键ID',
  job_id BIGINT NOT NULL COMMENT '所属批量查询任务ID',
  source_row INT NOT NULL COMMENT '身份证在Excel中的原始行号',
  id_card VARCHAR(32) NOT NULL COMMENT '待查询居民身份证号码',

  status VARCHAR(20) NOT NULL DEFAULT 'pending'
    COMMENT '明细状态：pending待查询、running查询中、success查询成功、not_found查无此人、failed查询失败',
  retry_count INT NOT NULL DEFAULT 0 COMMENT '当前明细已重试次数',
  error_message VARCHAR(1000) DEFAULT NULL COMMENT '查询失败原因或异常信息',

  claimed_at DATETIME NULL COMMENT '明细被Worker领取的时间',
  started_at DATETIME NULL COMMENT '明细开始调用业务接口的时间',
  completed_at DATETIME NULL COMMENT '明细查询完成时间',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '明细导入时间',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
    ON UPDATE CURRENT_TIMESTAMP COMMENT '明细最后更新时间',

  UNIQUE KEY uk_job_row (job_id, source_row),
  KEY idx_job_status_id (job_id, status, id),
  KEY idx_job_id_card (job_id, id_card),

  CONSTRAINT fk_batch_item_job
    FOREIGN KEY (job_id) REFERENCES batch_query_jobs(id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='居民档案批量查询身份证明细表';


-- 查询产生的调阅记录，一位居民可能有多条
CREATE TABLE IF NOT EXISTS batch_query_results (
  id BIGINT PRIMARY KEY AUTO_INCREMENT COMMENT '查询结果主键ID',
  job_id BIGINT NOT NULL COMMENT '所属批量查询任务ID',
  item_id BIGINT NOT NULL COMMENT '对应的身份证查询明细ID',

  id_card VARCHAR(32) NOT NULL COMMENT '居民身份证号码',
  person_name VARCHAR(100) DEFAULT NULL COMMENT '业务接口返回的居民姓名',
  record_index INT NOT NULL DEFAULT 1 COMMENT '同一居民调阅记录序号',

  view_time VARCHAR(32) DEFAULT NULL COMMENT '档案调阅时间',
  view_org_name VARCHAR(255) DEFAULT NULL COMMENT '档案调阅机构名称',
  department VARCHAR(255) DEFAULT NULL COMMENT '档案调阅科室名称',
  view_user_name VARCHAR(100) DEFAULT NULL COMMENT '档案调阅人姓名',
  access_channel VARCHAR(100) DEFAULT NULL COMMENT '档案调阅渠道',

  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '查询结果写入时间',

  KEY idx_job_item (job_id, item_id),
  KEY idx_job_id_card (job_id, id_card),

  CONSTRAINT fk_batch_result_job
    FOREIGN KEY (job_id) REFERENCES batch_query_jobs(id)
    ON DELETE CASCADE,

  CONSTRAINT fk_batch_result_item
    FOREIGN KEY (item_id) REFERENCES batch_query_items(id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='居民档案批量查询调阅记录结果表';
