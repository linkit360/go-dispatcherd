ALTER TABLE xmp_operators ADD COLUMN msisdn_headers jsonb NOT NULL DEFAULT '[]';
UPDATE xmp_operators SET msisdn_headers = '[ "HTTP_MSISDN" ]' where name = 'Mobilink';

ALTER TABLE xmp_operator_ip ADD COLUMN country_code INT NOT NULL DEFAULT 0;
UPDATE  xmp_operator_ip SET country_code = ( SELECT country_code FROM xmp_operators WHERE xmp_operator_ip.operator_code = xmp_operators.code)