ALTER TABLE tv_devices ADD COLUMN model TEXT NOT NULL DEFAULT '';
UPDATE tv_devices SET model = name;

WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY paired_at, id) AS n FROM tv_devices
)
UPDATE tv_devices SET name = (
    SELECT CASE WHEN n = 1 THEN '电视' ELSE '电视 ' || n END FROM ordered WHERE ordered.id = tv_devices.id
);
