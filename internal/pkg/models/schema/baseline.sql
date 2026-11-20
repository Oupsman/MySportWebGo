-- The schema of MySportWeb when the versioned migrations started: what GORM's AutoMigrate created from the
-- models (pg_dump --schema-only of a new database, without its session settings and the schema prefix).
-- Applied by the migration 1 to a new database only. Never edit it: a change of the schema is a new file in
-- migrations/.

CREATE TABLE activities (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    user_id bigint,
    title text,
    date timestamp with time zone,
    filename text,
    file_path text,
    sport text,
    is_commute boolean,
    co2 numeric,
    distance numeric,
    duration bigint,
    avg_speed numeric,
    avg_rpm smallint,
    avg_hr smallint,
    calories integer,
    timezone text DEFAULT 'Europe/Paris'::text,
    masked boolean DEFAULT true,
    equipment_id uuid,
    serialnumber text,
    lastimport boolean,
    visibility bigint DEFAULT 0,
    can_comments boolean DEFAULT false,
    engine_version bigint,
    medias bytea,
    max_speed numeric,
    max_speed_position bytea,
    total_weight integer,
    device bytea,
    device_name text,
    device_id uuid,
    user_age smallint,
    thumbnail text,
    speeds bytea,
    hearts bytea,
    powers bytea,
    power_ts bytea,
    avg_power integer,
    power_axis bytea,
    cadences bytea,
    distances bytea,
    lats bytea,
    lons bytea,
    gps_points bytea,
    public_gps_points bytea,
    simplified_gps_points bytea,
    route_id uuid,
    metrics jsonb,
    temperatures bytea,
    time_stamps bytea,
    gps_time_stamps bytea,
    heart_time_stamps bytea,
    cadence_time_stamps bytea,
    temperature_time_stamps bytea,
    physical_conditions bytea,
    total_ascent integer,
    total_descent integer,
    altitudes bytea,
    gps_center bytea,
    gps_bounds bytea,
    means bytea,
    positive_elevation numeric,
    negative_elevation numeric,
    elevation_correction text DEFAULT 'none'::text NOT NULL,
    start_position bytea,
    end_position bytea,
    lengths bytea,
    legs bytea,
    trim_end numeric,
    recorded_duration numeric,
    trimp numeric,
    hr_zones bytea,
    hr_zone_bounds bytea,
    best_efforts jsonb
);

CREATE TABLE channels (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    user_id bigint,
    channel_provider text,
    config text,
    name text
);

CREATE SEQUENCE channels_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE channels_id_seq OWNED BY channels.id;

CREATE TABLE device_tokens (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamp with time zone,
    user_id bigint NOT NULL,
    name text NOT NULL,
    hash text NOT NULL,
    hint text,
    permissions integer NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    last_used_at timestamp with time zone
);

CREATE TABLE devices (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    user_id bigint NOT NULL,
    brand text DEFAULT ''::text NOT NULL,
    model text DEFAULT ''::text NOT NULL,
    serial text DEFAULT ''::text NOT NULL,
    name text,
    altitude_correction text DEFAULT 'none'::text NOT NULL,
    merged_into_id uuid
);

CREATE TABLE equipments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    name text,
    brand text,
    equipment_model text,
    date_of_purchase timestamp with time zone,
    initial_mileage bigint,
    mileage bigint,
    weight bigint,
    user_id bigint,
    maintenance_interval bigint,
    is_default boolean,
    is_commute boolean,
    picture text
);

CREATE TABLE health_data (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    user_id bigint,
    date timestamp with time zone,
    weight numeric,
    fat numeric,
    muscle numeric,
    bone numeric,
    body_water numeric,
    rest_hr integer,
    vo2_max integer,
    systolic integer,
    diastolic integer
);

CREATE SEQUENCE health_data_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE health_data_id_seq OWNED BY health_data.id;

CREATE TABLE keys (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    pub_key text,
    priv_key text
);

CREATE SEQUENCE keys_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE keys_id_seq OWNED BY keys.id;

CREATE TABLE notifications (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    channel_id bigint,
    message text,
    sent boolean
);

CREATE SEQUENCE notifications_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE notifications_id_seq OWNED BY notifications.id;

CREATE TABLE revoked_sessions (
    id text NOT NULL,
    expires_at timestamp with time zone NOT NULL
);

CREATE TABLE routes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    user_id bigint,
    name text,
    sport text,
    distance numeric,
    activity_count bigint,
    is_commute boolean,
    start_position bytea,
    end_position bytea,
    simplified_gps_points bytea
);

CREATE TABLE services (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    equipment_id uuid NOT NULL,
    date timestamp with time zone,
    mileage bigint,
    description text
);

CREATE TABLE users (
    id bigint NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    uuid uuid DEFAULT gen_random_uuid(),
    username text,
    password text,
    role text,
    email text,
    date_of_birth timestamp with time zone,
    weight integer DEFAULT 70,
    unit bigint,
    timezone text DEFAULT 'Europe/Paris'::text,
    gender bigint DEFAULT 0,
    height integer,
    max_hr integer DEFAULT 0,
    weight_objective numeric DEFAULT 0,
    security_distance integer DEFAULT 500,
    home_town text DEFAULT 'Earth'::text,
    sessions_valid_after timestamp with time zone
);

CREATE SEQUENCE users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE users_id_seq OWNED BY users.id;

CREATE TABLE validations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    user_id bigint,
    validated boolean
);

ALTER TABLE ONLY channels ALTER COLUMN id SET DEFAULT nextval('channels_id_seq'::regclass);

ALTER TABLE ONLY health_data ALTER COLUMN id SET DEFAULT nextval('health_data_id_seq'::regclass);

ALTER TABLE ONLY keys ALTER COLUMN id SET DEFAULT nextval('keys_id_seq'::regclass);

ALTER TABLE ONLY notifications ALTER COLUMN id SET DEFAULT nextval('notifications_id_seq'::regclass);

ALTER TABLE ONLY users ALTER COLUMN id SET DEFAULT nextval('users_id_seq'::regclass);

ALTER TABLE ONLY activities
    ADD CONSTRAINT activities_pkey PRIMARY KEY (id);

ALTER TABLE ONLY channels
    ADD CONSTRAINT channels_pkey PRIMARY KEY (id);

ALTER TABLE ONLY device_tokens
    ADD CONSTRAINT device_tokens_pkey PRIMARY KEY (id);

ALTER TABLE ONLY devices
    ADD CONSTRAINT devices_pkey PRIMARY KEY (id);

ALTER TABLE ONLY equipments
    ADD CONSTRAINT equipments_pkey PRIMARY KEY (id);

ALTER TABLE ONLY health_data
    ADD CONSTRAINT health_data_pkey PRIMARY KEY (id);

ALTER TABLE ONLY keys
    ADD CONSTRAINT keys_pkey PRIMARY KEY (id);

ALTER TABLE ONLY notifications
    ADD CONSTRAINT notifications_pkey PRIMARY KEY (id);

ALTER TABLE ONLY revoked_sessions
    ADD CONSTRAINT revoked_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY routes
    ADD CONSTRAINT routes_pkey PRIMARY KEY (id);

ALTER TABLE ONLY services
    ADD CONSTRAINT services_pkey PRIMARY KEY (id);

ALTER TABLE ONLY users
    ADD CONSTRAINT uni_users_email UNIQUE (email);

ALTER TABLE ONLY users
    ADD CONSTRAINT uni_users_username UNIQUE (username);

ALTER TABLE ONLY users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

ALTER TABLE ONLY validations
    ADD CONSTRAINT validations_pkey PRIMARY KEY (id);

CREATE INDEX idx_activities_device_id ON activities USING btree (device_id);

CREATE INDEX idx_activities_device_name ON activities USING btree (device_name);

CREATE INDEX idx_activities_route_id ON activities USING btree (route_id);

CREATE INDEX idx_channels_deleted_at ON channels USING btree (deleted_at);

CREATE UNIQUE INDEX idx_device_tokens_hash ON device_tokens USING btree (hash);

CREATE INDEX idx_device_tokens_user_id ON device_tokens USING btree (user_id);

CREATE INDEX idx_devices_merged_into_id ON devices USING btree (merged_into_id);

CREATE UNIQUE INDEX idx_devices_unit ON devices USING btree (user_id, brand, model, serial);

CREATE INDEX idx_health_data_deleted_at ON health_data USING btree (deleted_at);

CREATE INDEX idx_keys_deleted_at ON keys USING btree (deleted_at);

CREATE INDEX idx_notifications_deleted_at ON notifications USING btree (deleted_at);

CREATE INDEX idx_revoked_sessions_expires_at ON revoked_sessions USING btree (expires_at);

CREATE INDEX idx_routes_user_id ON routes USING btree (user_id);

CREATE INDEX idx_services_equipment_id ON services USING btree (equipment_id);

CREATE INDEX idx_users_deleted_at ON users USING btree (deleted_at);

ALTER TABLE ONLY activities
    ADD CONSTRAINT fk_activities_equipment FOREIGN KEY (equipment_id) REFERENCES equipments(id);

ALTER TABLE ONLY activities
    ADD CONSTRAINT fk_activities_user FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE ONLY channels
    ADD CONSTRAINT fk_channels_user FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE ONLY device_tokens
    ADD CONSTRAINT fk_device_tokens_user FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE ONLY devices
    ADD CONSTRAINT fk_devices_user FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE ONLY equipments
    ADD CONSTRAINT fk_equipments_user FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE ONLY health_data
    ADD CONSTRAINT fk_health_data_user FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE ONLY notifications
    ADD CONSTRAINT fk_notifications_channel FOREIGN KEY (channel_id) REFERENCES channels(id);

ALTER TABLE ONLY services
    ADD CONSTRAINT fk_services_equipment FOREIGN KEY (equipment_id) REFERENCES equipments(id) ON DELETE CASCADE;

ALTER TABLE ONLY validations
    ADD CONSTRAINT fk_validations_user FOREIGN KEY (user_id) REFERENCES users(id);
