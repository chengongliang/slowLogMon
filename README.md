# MySQL 慢日志监控报警和入库

1. 通过 filebeat 收集慢日志 到 redis
配置如下：

```yaml
filebeat.prospectors:
- input_type: log
  paths:
    - /u01/mysql/3306/log/slow/mysql-slow.log
    - /u01/mysql/3307/log/slow/mysql-slow.log
    - /u01/mysql/3308/log/slow/mysql-slow.log
    - /u01/mysql/3309/log/slow/mysql-slow.log
    - /u01/mysql/3310/log/slow/mysql-slow.log
  multiline:
    pattern: '^(# User@Host: |# Time: )'
    negate: true
    match: after
  exclude_lines: ['^[\/\w\.]+, Version: .* started with:.*', '^# Time:.*']   # Exclude the header and time
  fields:
    host: 'mysql_host'
  fields_under_root: true
  tail_files: true

output.redis:
   hosts: ["redis_host"]
   port: 6379
   key: "mysql-slow-log"
```

2. slowLogMon 监控 redis **mysql-slow-log** 根据配置报警和入库

slow_logs 结构：

```sql
CREATE TABLE `slow_logs` (
  `id` int(11) unsigned NOT NULL AUTO_INCREMENT,
  `time` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `target` varchar(32) DEFAULT NULL COMMENT '数据库地址',
  `user` varchar(16) DEFAULT NULL,
  `ip` varchar(32) DEFAULT NULL,
  `database` varchar(16) DEFAULT NULL,
  `query_time` float DEFAULT NULL,
  `lock_time` float DEFAULT NULL,
  `rows_sent` int(16) DEFAULT NULL,
  `rows_examined` int(16) DEFAULT NULL,
  `rows_affected` int(16) DEFAULT NULL,
  `bytes_sent` int(16) DEFAULT NULL,
  `sql` varchar(1024) DEFAULT NULL,
  `stat` int(2) DEFAULT '0' COMMENT '0 未处理，1 已处理',
  `comment` varchar(128) DEFAULT NULL COMMENT '备注',
  `handler` varchar(64) DEFAULT NULL COMMENT '处理人',
  `finish_time` timestamp NULL DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

